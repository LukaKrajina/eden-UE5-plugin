package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	lp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/syndtr/goleveldb/leveldb"
)

const (
	TxTypeStake          = "STAKE_FUNDS"
	TxTypeTribunal       = "TRIBUNAL_VERDICT"
	TxTypeRegisterFriend = "REGISTER_FRIEND"
	TxTypeTransfer       = "TRANSFER"
	TxTypeBet            = "BET"
	TxTypeMatchStart     = "MATCH_START"
	TxTypeMatchAbort     = "MATCH_ABORT"
	TxTypeWitness        = "MATCH_WITNESS"
	TxTypeUpdateProfile  = "UPDATE_PROFILE"
	TxTypeMatchResult    = "MATCH_RESULT"
	TxTypePenalty        = "MATCH_PENALTY"
	TxTypeResolve        = "RESOLVE_PAYOUT"
	AbelScale            = 173.7178
	Tau                  = 0.5
)

type Transaction struct {
	ID        string  `json:"id"`
	Type      string  `json:"type"`
	Sender    string  `json:"sender"`
	Receiver  string  `json:"receiver"`
	Amount    float64 `json:"amount"`
	Payload   string  `json:"payload"`
	Timestamp int64   `json:"timestamp"`
	Signature string  `json:"signature"`
	PublicKey []byte  `json:"pub_key"`
	Nonce     uint64  `json:"nonce"`
}

type GameProof struct {
	MatchID       string   `json:"match_id"`
	Duration      int      `json:"duration"`
	MaxPlayers    int      `json:"max_players"`
	QualityScore  int      `json:"quality_score"`
	PlayerWitness []string `json:"witnesses"`
}

type UserProfile struct {
	PeerID              string  `json:"peer_id"`
	Username            string  `json:"username"`
	AvatarURL           string  `json:"avatar_url"`
	XP                  float64 `json:"xp"`
	Level               int     `json:"level"`
	Rating              float64 `json:"rating"`
	AvgRating           float64 `json:"avg_rating"`
	Matches             int     `json:"matches"`
	Wins                int     `json:"wins"`
	Deviation           float64 `json:"deviation"`
	Volatility          float64 `json:"volatility"`
	StakedEDN           float64 `json:"staked_edn"`
	TribunalDemosParsed int     `json:"tribunal_demos_parsed"`
	TribunalEDNEarned   float64 `json:"tribunal_edn_earned"`
	TribunalCorrect     int     `json:"tribunal_correct"`
	TribunalTotalVotes  int     `json:"tribunal_total_votes"`
}

type MatchSessionInfo struct {
	HostID     string
	StartTime  int64
	Roster     []string
	MatchStake float64
	State      string
	Witnesses  map[string]WitnessVote
}

type WitnessVote struct {
	WinnerTeam string
	ReplayHash string
}

type Bet struct {
	Bettor  string  `json:"bettor"`
	Amount  float64 `json:"amount"`
	Team    string  `json:"team"`
	MatchID string  `json:"match_id"`
}

type BettingPool struct {
	MatchID   string  `json:"match_id"`
	TotalPool float64 `json:"total_pool"`
	TeamAPool float64 `json:"team_a_pool"`
	TeamBPool float64 `json:"team_b_pool"`
	IsOpen    bool    `json:"is_open"`
	Bets      []Bet   `json:"bets"`
}

type Block struct {
	Index         int               `json:"index"`
	Timestamp     int64             `json:"timestamp"`
	Transactions  []Transaction     `json:"transactions"`
	GameData      *GameProof        `json:"game_data,omitempty"`
	PrevHash      string            `json:"prev_hash"`
	Hash          string            `json:"hash"`
	ValidatorSigs map[string]string `json:"validator_sigs"`
	ChainWeight   float64           `json:"chain_weight"`
}

type Blockchain struct {
	Database       *leveldb.DB
	DBPath         string
	LastBlock      Block
	Balances       map[string]float64
	Profiles       map[string]*UserProfile
	ActivePools    map[string]*BettingPool
	AccountNonces  map[string]uint64
	MatchSessions  map[string]MatchSessionInfo
	MatchVotes     map[string]map[string]string `json:"match_votes"`
	QueueBans      map[string]int64             `json:"queue_bans"`
	FriendRegistry map[string]string            `json:"friend_registry"`
	PublicKeys     map[string]string
	TribunalVotes  map[string]map[string]map[string]bool
	Mutex          sync.RWMutex
}

type TribunalProposal struct {
	MatchID     string `json:"match_id"`
	SuspectID   string `json:"suspect_id"`
	IsGuilty    bool   `json:"is_guilty"`
	ValidatorID string `json:"validator_id"`
	Signature   string `json:"signature"`
}

var EdenChain *Blockchain

func InitializeChain(dbPath string) {
	db, err := leveldb.OpenFile(dbPath, nil)
	if err != nil {
		panic("Failed to open LevelDB: " + err.Error())
	}

	EdenChain = &Blockchain{
		Balances:       make(map[string]float64),
		Profiles:       make(map[string]*UserProfile),
		ActivePools:    make(map[string]*BettingPool),
		AccountNonces:  make(map[string]uint64),
		MatchVotes:     make(map[string]map[string]string),
		QueueBans:      make(map[string]int64),
		MatchSessions:  make(map[string]MatchSessionInfo),
		FriendRegistry: make(map[string]string),
		PublicKeys:     make(map[string]string),
		Database:       db,
		DBPath:         dbPath,
	}
	EdenChain.LoadFromDB()
}

func (bc *Blockchain) SaveBlockToDB(b Block) {
	data, _ := json.Marshal(b)
	key := fmt.Sprintf("block_%d", b.Index)
	batch := new(leveldb.Batch)
	batch.Put([]byte(key), data)
	batch.Put([]byte("latest_index"), []byte(strconv.Itoa(b.Index)))
	bc.Database.Write(batch, nil)
}

func (bc *Blockchain) LoadFromDB() {
	latestBytes, err := bc.Database.Get([]byte("latest_index"), nil)
	if err != nil {
		fmt.Println("[DB] No history found. Creating Genesis Block.")
		genesis := Block{
			Index: 0, Timestamp: 1704067200, Hash: "GENESIS_BLOCK", PrevHash: "0",
		}
		bc.AddBlock(genesis)
		return
	}

	latestIndex, _ := strconv.Atoi(string(latestBytes))
	fmt.Printf("[DB] Found chain history up to height %d. Replaying...\n", latestIndex)

	for i := 0; i <= latestIndex; i++ {
		key := fmt.Sprintf("block_%d", i)
		data, err := bc.Database.Get([]byte(key), nil)
		if err != nil {
			fmt.Printf("[DB] Error corrupted chain at block %d\n", i)
			break
		}

		var b Block
		json.Unmarshal(data, &b)

		bc.ProcessBlockState(b)
		bc.LastBlock = b
	}
	fmt.Println("[DB] State restoration complete.")
}

func GenerateFixedUsername(peerID string) string {
	hash := sha256.Sum256([]byte(peerID))
	num := binary.BigEndian.Uint32(hash[:4])
	sequence := num % 1000000
	return fmt.Sprintf("User%06d", sequence)
}

func (bc *Blockchain) GetOrInitProfile(peerID string) *UserProfile {
	if profile, exists := bc.Profiles[peerID]; exists {
		return profile
	}

	newProfile := &UserProfile{
		PeerID:     peerID,
		Username:   GenerateFixedUsername(peerID),
		AvatarURL:  "",
		XP:         0,
		Level:      0,
		Rating:     1500.0,
		AvgRating:  1.0,
		Matches:    0,
		Wins:       0,
		Deviation:  350.0,
		Volatility: 0.06,
	}
	bc.Profiles[peerID] = newProfile
	return newProfile
}

func (tx *Transaction) GenerateTxHash() []byte {
	record := fmt.Sprintf("%s%s%s%f%d%s%d", tx.Sender, tx.PublicKey, tx.Receiver, tx.Amount, tx.Timestamp, tx.Payload, tx.Nonce)
	h := sha256.New()
	h.Write([]byte(record))
	return h.Sum(nil)
}

func VerifyTransaction(tx Transaction) bool {
	var pubKeyBytes []byte
	if len(tx.PublicKey) > 0 {
		pubKeyBytes = tx.PublicKey
	} else {
		return false
	}

	genericPublicKey, err := x509.ParsePKIXPublicKey(pubKeyBytes)
	if err != nil {
		fmt.Printf("[Crypto] Failed to parse PKIX Public Key: %v\n", err)
		return false
	}

	pubKey, ok := genericPublicKey.(*ecdsa.PublicKey)
	if !ok {
		fmt.Printf("[Crypto] Public Key is not ECDSA\n")
		return false
	}

	lp2pPubKey, err := lp2pcrypto.UnmarshalECDSAPublicKey(pubKeyBytes)
	if err != nil {
		fmt.Printf("[Crypto] Failed to unmarshal libp2p public key for %s\n", tx.ID)
		return false
	}

	derivedPeerID, err := peer.IDFromPublicKey(lp2pPubKey)
	if err != nil {
		fmt.Printf("[Crypto] Failed to derive Peer ID for %s\n", tx.ID)
		return false
	}

	if derivedPeerID.String() != tx.Sender {
		fmt.Printf("[Crypto] FRAUD: Public Key mathematically belongs to %s, but Sender claims to be %s\n", derivedPeerID.String(), tx.Sender)
		return false
	}

	sigBytes, err := hex.DecodeString(tx.Signature)
	if err != nil || len(sigBytes) == 0 {
		fmt.Printf("[Crypto] Invalid Signature Hex\n")
		return false
	}

	r := big.NewInt(0).SetBytes(sigBytes[:len(sigBytes)/2])
	s := big.NewInt(0).SetBytes(sigBytes[len(sigBytes)/2:])
	hash := tx.GenerateTxHash()
	return ecdsa.Verify(pubKey, hash, r, s)
}

func (bc *Blockchain) AddBlock(b Block) bool {
	bc.Mutex.Lock()
	defer bc.Mutex.Unlock()
	lastBlock := bc.LastBlock
	if b.PrevHash != lastBlock.Hash {
		return false
	}

	if b.Index != lastBlock.Index+1 {
		return false
	}

	if !bc.ProcessBlockState(b) {
		fmt.Println("[Chain] Block rejected due to invalid state transition")
		return false
	}

	bc.LastBlock = b
	bc.SaveBlockToDB(b)
	return true
}

func (bc *Blockchain) ProcessBlockState(b Block) bool {
	tempBalances := make(map[string]float64)
	tempNonces := make(map[string]uint64)

	for _, tx := range b.Transactions {
		isSystemTx := tx.Sender == "SYSTEM_MINT" || tx.Sender == "SYSTEM_PAYOUT"
		if !isSystemTx {
			if !VerifyTransaction(tx) {
				return false
			}

			currentNonce := bc.AccountNonces[tx.Sender]
			if tempVal, exists := tempNonces[tx.Sender]; exists {
				currentNonce = tempVal
			}
			if tx.Nonce != currentNonce+1 {
				return false
			}

			currentBal := bc.Balances[tx.Sender]
			if tempVal, exists := tempBalances[tx.Sender]; exists {
				currentBal = tempVal
			}
			if currentBal < tx.Amount {
				return false
			}

			tempBalances[tx.Sender] = currentBal - tx.Amount
			tempNonces[tx.Sender] = currentNonce + 1
		}
	}

	for _, tx := range b.Transactions {

		isSystemTx := tx.Sender == "SYSTEM_MINT" || tx.Sender == "SYSTEM_PAYOUT"

		if len(tx.PublicKey) > 0 {
			bc.PublicKeys[tx.Sender] = hex.EncodeToString(tx.PublicKey)
		}

		if !isSystemTx {
			bc.Balances[tx.Sender] -= tx.Amount
			bc.AccountNonces[tx.Sender]++
		}

		switch tx.Type {

		case TxTypeUpdateProfile:
			if tx.Payload != "" {
				parts := strings.Split(tx.Payload, "|")
				profile := bc.GetOrInitProfile(tx.Sender)

				if len(parts) >= 2 {
					profile.Username = parts[0]
					profile.AvatarURL = parts[1]
				} else {
					profile.AvatarURL = parts[0]
				}
			}

		case TxTypeMatchResult:
			if tx.Sender != "SYSTEM_PAYOUT" && tx.Sender != "CONSENSUS_ENGINE" {
				fmt.Printf("[Security] REJECTED: Forged Match Result from %s\n", tx.Sender)
				continue
			}
			var res struct {
				TargetID      string  `json:"target_id"`
				Win           bool    `json:"win"`
				RatingScore   float64 `json:"rating_score"`
				TeamAvgRating float64 `json:"team_avg_rating"`
				TeamAvgDev    float64 `json:"team_avg_dev"`
			}
			if err := json.Unmarshal([]byte(tx.Payload), &res); err == nil {
				bc.processMatchProgression(res.TargetID, res.Win, res.RatingScore, res.TeamAvgRating, res.TeamAvgDev)
			}

		case TxTypePenalty:
			parts := strings.Split(tx.Payload, ":")
			if len(parts) >= 2 {
				matchID := parts[0]
				dodgerID := parts[1]

				session, exists := bc.MatchSessions[matchID]

				if exists && session.HostID == tx.Sender {
					bc.QueueBans[dodgerID] = b.Timestamp + 300
					profile := bc.GetOrInitProfile(dodgerID)
					profile.Rating -= 15.0
					if profile.Rating < 100.0 {
						profile.Rating = 100.0
					}
					fmt.Printf("[Leaver Buster] Host %s penalized %s for dodging %s. -15 Rating, 5m cooldown.\n", tx.Sender, dodgerID, matchID)
				} else {
					fmt.Printf("[Leaver Buster] REJECTED: Sender %s is not the host of %s.\n", tx.Sender, matchID)
				}
			}

		case TxTypeRegisterFriend:
			code := tx.Payload
			bc.FriendRegistry[code] = tx.Sender

		case TxTypeTransfer:
			bc.Balances[tx.Receiver] += tx.Amount

		case TxTypeBet:
			bc.processBet(tx)

		case TxTypeResolve:
			bc.Balances[tx.Receiver] += tx.Amount

		case TxTypeMatchStart:
			parts := strings.Split(tx.Payload, "|")
			if len(parts) >= 2 {
				matchID := parts[0]
				players := strings.Split(parts[1], ",")
				bc.MatchSessions[matchID] = MatchSessionInfo{
					HostID:    tx.Sender,
					StartTime: tx.Timestamp,
					Roster:    players,
				}
				bc.MatchVotes[matchID] = make(map[string]string)
				fmt.Printf("[Consensus] Match %s started with %d players.\n", matchID, len(players))
			}

		case TxTypeMatchAbort:
			matchID := tx.Payload
			session, exists := bc.MatchSessions[matchID]

			if exists && session.HostID == tx.Sender {
				if pool, pExists := bc.ActivePools[matchID]; pExists {
					for _, bet := range pool.Bets {
						bc.Balances[bet.Bettor] += bet.Amount
					}
					delete(bc.ActivePools, matchID)
				}
				delete(bc.MatchSessions, matchID)
				delete(bc.MatchVotes, matchID)
			}

		case TxTypeWitness:
			parts := strings.Split(tx.Payload, ":")
			if len(parts) < 2 {
				continue
			}

			matchID := parts[0]
			votedWinner := parts[1]
			votedMVP := "NONE"
			if len(parts) > 2 {
				votedMVP = parts[2]
			}

			session, exists := bc.MatchSessions[matchID]
			if !exists {
				continue
			}

			replayHash := "NO_HASH"
			if len(parts) > 4 {
				replayHash = parts[4]
			}

			if session.Witnesses == nil {
				session.Witnesses = make(map[string]WitnessVote)
			}
			session.Witnesses[tx.Sender] = WitnessVote{
				WinnerTeam: votedWinner,
				ReplayHash: replayHash,
			}
			bc.MatchSessions[matchID] = session

			isParticipant := false
			for _, p := range session.Roster {
				if p == tx.Sender {
					isParticipant = true
					break
				}
			}
			if !isParticipant {
				continue
			}

			voteKey := fmt.Sprintf("%s|%s", votedWinner, votedMVP)
			bc.MatchVotes[matchID][tx.Sender] = voteKey

			voteCounts := make(map[string]int)
			for _, v := range bc.MatchVotes[matchID] {
				voteCounts[v]++
			}

			if voteCounts[voteKey] > len(session.Roster)/2 {
				fmt.Printf("[Consensus] Match %s Finalized. Winner: %s\n", matchID, votedWinner)

				duration := tx.Timestamp - session.StartTime
				if duration < 0 {
					duration = 600
				}

				seed := b.Timestamp
				rng := math.Round(float64(seed)) * 0.000011574074
				hostReward := float64(duration) * rng

				mintTx := Transaction{
					ID:        fmt.Sprintf("mint_%s", matchID),
					Type:      TxTypeTransfer,
					Sender:    "SYSTEM_MINT",
					Receiver:  session.HostID,
					Amount:    hostReward,
					Timestamp: time.Now().Unix(),
					Signature: "CONSENSUS_REWARD",
				}

				bc.Balances[mintTx.Receiver] += mintTx.Amount

				bettingTxs := bc.ResolveMatch(matchID, votedWinner)
				for _, bTx := range bettingTxs {
					bc.Balances[bTx.Receiver] += bTx.Amount
				}

				playerRatings := make(map[string]float64)
				playerTeams := make(map[string]string)

				if len(parts) > 3 {
					alignments := strings.Split(parts[3], ",")
					for _, align := range alignments {
						ap := strings.Split(align, "=")
						// Now uses PeerID directly: PeerID=Team=Rating
						if len(ap) == 3 {
							pID := ap[0]
							team := ap[1]
							rating, _ := strconv.ParseFloat(ap[2], 64)
							playerRatings[pID] = rating
							playerTeams[pID] = team
						}
					}
				}

				bc.DistributePlayerXP(matchID, session.Roster, votedWinner, votedMVP, playerRatings, playerTeams)

				delete(bc.MatchSessions, matchID)
				delete(bc.MatchVotes, matchID)
			}

		case TxTypeStake:
			profile := bc.GetOrInitProfile(tx.Sender)
			profile.StakedEDN += tx.Amount

		case TxTypeTribunal:
			parts := strings.Split(tx.Payload, ":")
			if len(parts) >= 3 {
				matchID := parts[0]
				suspectID := parts[1]
				isGuilty := parts[2] == "1"

				if bc.TribunalVotes[matchID] == nil {
					bc.TribunalVotes[matchID] = make(map[string]map[string]bool)
				}
				if bc.TribunalVotes[matchID][suspectID] == nil {
					bc.TribunalVotes[matchID][suspectID] = make(map[string]bool)
				}

				bc.TribunalVotes[matchID][suspectID][tx.Sender] = isGuilty

				var guiltyWeight, innocentWeight float64
				for validatorID, vote := range bc.TribunalVotes[matchID][suspectID] {
					profile := bc.GetOrInitProfile(validatorID)
					stake := profile.StakedEDN
					if vote {
						guiltyWeight += stake
					} else {
						innocentWeight += stake
					}
				}

				totalWeight := guiltyWeight + innocentWeight

				if totalWeight >= 1000.0 {
					if (guiltyWeight / totalWeight) > 0.66 {
						suspectProfile := bc.GetOrInitProfile(suspectID)
						suspectProfile.Rating = 100.0
						bc.Balances[suspectID] = 0
						bc.QueueBans[suspectID] = b.Timestamp + 315360000
						bc.ProcessValidatorPayouts(matchID, suspectID, true)
						delete(bc.TribunalVotes[matchID], suspectID)
					} else if (innocentWeight / totalWeight) > 0.66 {
						bc.ProcessValidatorPayouts(matchID, suspectID, false)
						delete(bc.TribunalVotes[matchID], suspectID)
					}
				}
			}
		}
	}
	return true
}

func abelG(phi float64) float64 {
	return 1.0 / math.Sqrt(1.0+3.0*phi*phi/(math.Pi*math.Pi))
}

func abelE(mu, mu_j, phi_j float64) float64 {
	return 1.0 / (1.0 + math.Exp(-abelG(phi_j)*(mu-mu_j)))
}

func CalculateAbel2(pRating, pDev, oppRating, oppDev float64, outcome float64) (float64, float64) {
	mu := (pRating - 1500.0) / AbelScale
	phi := pDev / AbelScale
	mu_j := (oppRating - 1500.0) / AbelScale
	phi_j := oppDev / AbelScale

	g_phi_j := abelG(phi_j)
	e_val := abelE(mu, mu_j, phi_j)

	v := 1.0 / (g_phi_j * g_phi_j * e_val * (1.0 - e_val))

	delta := v * g_phi_j * (outcome - e_val)

	phiStar := math.Sqrt(phi*phi + 0.06*0.06)
	newPhi := 1.0 / math.Sqrt((1.0/(phiStar*phiStar))+(1.0/v))
	newMu := mu + newPhi*newPhi*(delta/v)

	finalRating := (newMu * AbelScale) + 1500.0
	finalDev := newPhi * AbelScale

	if finalDev < 30.0 {
		finalDev = 30.0
	}

	return finalRating, finalDev
}

func (bc *Blockchain) ProcessValidatorPayouts(matchID string, suspectID string, consensusWasGuilty bool) {
	slashPercentage := 0.20
	var totalSlashedFunds float64
	var honestValidators []string

	for validatorID, vote := range bc.TribunalVotes[matchID][suspectID] {
		profile := bc.GetOrInitProfile(validatorID)
		profile.TribunalTotalVotes++
		profile.TribunalDemosParsed++

		if vote != consensusWasGuilty {
			penalty := profile.StakedEDN * slashPercentage
			profile.StakedEDN -= penalty
			totalSlashedFunds += penalty
		} else {
			honestValidators = append(honestValidators, validatorID)
			profile.TribunalCorrect++
		}
	}

	if len(honestValidators) > 0 && totalSlashedFunds > 0 {
		var totalHonestStake float64
		var shareRatio float64 = 0
		for _, vID := range honestValidators {
			totalHonestStake += bc.GetOrInitProfile(vID).StakedEDN
		}

		for _, vID := range honestValidators {
			profile := bc.GetOrInitProfile(vID)
			if totalHonestStake > 0 {
				shareRatio = profile.StakedEDN / totalHonestStake
			}
			reward := totalSlashedFunds * shareRatio
			bc.Balances[vID] += reward
			profile.TribunalEDNEarned += reward
		}
	}
}

func (bc *Blockchain) DistributePlayerXP(matchID string, roster []string, winnerTeam string, mvpPeerID string, playerRatings map[string]float64, playerTeams map[string]string) {
	var teamATotalRating, teamATotalDev, teamBTotalRating, teamBTotalDev float64
	var teamACount, teamBCount float64

	for _, peerID := range roster {
		profile := bc.GetOrInitProfile(peerID)
		team := playerTeams[peerID]

		if team == "A" {
			teamATotalRating += profile.Rating
			teamATotalDev += profile.Deviation
			teamACount++
		} else if team == "B" {
			teamBTotalRating += profile.Rating
			teamBTotalDev += profile.Deviation
			teamBCount++
		}
	}

	teamAAvgRating, teamAAvgDev := 1500.0, 350.0
	if teamACount > 0 {
		teamAAvgRating = teamATotalRating / teamACount
		teamAAvgDev = teamATotalDev / teamACount
	}

	teamBAvgRating, teamBAvgDev := 1500.0, 350.0
	if teamBCount > 0 {
		teamBAvgRating = teamBTotalRating / teamBCount
		teamBAvgDev = teamBTotalDev / teamBCount
	}

	for _, peerID := range roster {
		playerTeam := playerTeams[peerID]
		didWin := (playerTeam == winnerTeam)
		rating := playerRatings[peerID]
		if rating == 0 {
			rating = 1.0
		}

		var oppTeamAvgRating, oppTeamAvgDev float64
		if playerTeam == "A" {
			oppTeamAvgRating = teamBAvgRating
			oppTeamAvgDev = teamBAvgDev
		} else {
			oppTeamAvgRating = teamAAvgRating
			oppTeamAvgDev = teamAAvgDev
		}

		bc.processMatchProgression(peerID, didWin, rating, oppTeamAvgRating, oppTeamAvgDev)
	}
}

func DeriveSharedAESKey(myPrivHex string, peerPubHex string) ([]byte, error) {
	privBytes, _ := hex.DecodeString(myPrivHex)
	pubBytes, _ := hex.DecodeString(peerPubHex)

	privKey, err := x509.ParseECPrivateKey(privBytes)
	if err != nil {
		return nil, err
	}

	genericPubKey, err := x509.ParsePKIXPublicKey(pubBytes)
	if err != nil {
		return nil, err
	}
	pubKey := genericPubKey.(*ecdsa.PublicKey)

	myECDH, err := privKey.ECDH()
	if err != nil {
		return nil, err
	}
	peerECDH, err := pubKey.ECDH()
	if err != nil {
		return nil, err
	}

	sharedSecret, err := myECDH.ECDH(peerECDH)
	if err != nil {
		return nil, err
	}

	hash := sha256.Sum256(sharedSecret)
	return hash[:], nil
}

func EncryptPassword(aesKey []byte, password string) string {
	block, _ := aes.NewCipher(aesKey)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	rand.Read(nonce)
	ciphertext := gcm.Seal(nonce, nonce, []byte(password), nil)
	return base64.StdEncoding.EncodeToString(ciphertext)
}

func DecryptPassword(aesKey []byte, encryptedBase64 string) string {
	data, _ := base64.StdEncoding.DecodeString(encryptedBase64)
	block, _ := aes.NewCipher(aesKey)
	gcm, _ := cipher.NewGCM(block)
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return ""
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return ""
	}
	return string(plaintext)
}

func (bc *Blockchain) processMatchProgression(peerID string, win bool, gameRating float64, oppTeamAvgRating float64, oppTeamAvgDev float64) {
	profile := bc.GetOrInitProfile(peerID)

	profile.Matches++
	if win {
		profile.Wins++
	}
	currentTotal := profile.AvgRating * float64(profile.Matches-1)
	profile.AvgRating = (currentTotal + gameRating) / float64(profile.Matches)

	multiplier := 1.0
	if gameRating > 1.0 {
		multiplier = gameRating
	}

	xpGain := 100.0 * multiplier
	if win {
		xpGain += 50.0
	}

	profile.XP += xpGain

	newLevel := int(math.Sqrt(profile.XP/100.0)) + 1
	profile.Level = newLevel

	outcome := 0.0
	if win {
		outcome = 1.0
	}

	newRating, newDev := CalculateAbel2(profile.Rating, profile.Deviation, oppTeamAvgRating, oppTeamAvgDev, outcome)

	scoreDelta := 0.0
	const BaseScoreChange = 15.0
	expectedScore := 1.0 / (1.0 + math.Pow(10, (oppTeamAvgRating-profile.Rating)/400.0))

	var actualScore float64
	if win {
		actualScore = 1.0
	} else {
		actualScore = 0.0
	}

	scoreDelta = BaseScoreChange * (actualScore - expectedScore)

	safePlayerRating := gameRating
	if safePlayerRating < 0.1 {
		safePlayerRating = 0.1
	}

	performanceModifier := scoreDelta * safePlayerRating
	profile.Rating = newRating + performanceModifier
	profile.Deviation = newDev
}

func (bc *Blockchain) processBet(tx Transaction) {
	var matchID, team string
	fmt.Sscanf(tx.Payload, "%s:%s", &matchID, &team)

	originalBalances := make(map[string]float64)
	for k, v := range bc.Balances {
		originalBalances[k] = v
	}

	bc.Balances[tx.Sender] -= tx.Amount

	pool, exists := bc.ActivePools[matchID]
	poolCreated := false
	if !exists {
		pool = &BettingPool{MatchID: matchID, IsOpen: true}
		bc.ActivePools[matchID] = pool
		poolCreated = true
	}

	if !pool.IsOpen {
		bc.Balances = originalBalances
		if poolCreated {
			delete(bc.ActivePools, matchID)
		}
		return
	}

	newBet := Bet{Bettor: tx.Sender, Amount: tx.Amount, Team: team, MatchID: matchID}
	pool.Bets = append(pool.Bets, newBet)
	pool.TotalPool += tx.Amount
	if team == "A" {
		pool.TeamAPool += tx.Amount
	} else if team == "B" {
		pool.TeamBPool += tx.Amount
	}
}

func (bc *Blockchain) ResolveMatch(matchID string, winningTeam string) []Transaction {
	pool, exists := bc.ActivePools[matchID]
	if !exists || !pool.IsOpen {
		return nil
	}

	pool.IsOpen = false
	payoutTxs := []Transaction{}

	var winningPoolTotal, losingPoolTotal float64
	if winningTeam == "A" {
		winningPoolTotal = pool.TeamAPool
		losingPoolTotal = pool.TeamBPool
	} else {
		winningPoolTotal = pool.TeamBPool
		losingPoolTotal = pool.TeamAPool
	}

	fmt.Print("%d", losingPoolTotal)

	const CommissionRate = 0.04
	totalPot := pool.TotalPool
	netPot := totalPot * (1.0 - CommissionRate)

	if winningPoolTotal == 0 {
		for i, bet := range pool.Bets {
			tx := Transaction{
				ID:        fmt.Sprintf("refund_%s_%d_%d", matchID, time.Now().UnixNano(), i),
				Type:      TxTypeResolve,
				Sender:    "SYSTEM_PAYOUT",
				Receiver:  bet.Bettor,
				Amount:    bet.Amount,
				Timestamp: time.Now().Unix(),
				Signature: "CONSENSUS_VERIFIED",
			}
			payoutTxs = append(payoutTxs, tx)
		}
		delete(bc.ActivePools, matchID)
		return payoutTxs
	}

	for _, bet := range pool.Bets {
		if bet.Team == winningTeam {
			share := bet.Amount / winningPoolTotal
			payoutAmount := share * netPot

			tx := Transaction{
				ID:        fmt.Sprintf("pay_%s_%d", matchID, time.Now().UnixNano()),
				Type:      TxTypeResolve,
				Sender:    "SYSTEM_PAYOUT",
				Receiver:  bet.Bettor,
				Amount:    payoutAmount,
				Timestamp: time.Now().Unix(),
				Signature: "CONSENSUS_VERIFIED",
			}
			payoutTxs = append(payoutTxs, tx)
		}
	}

	delete(bc.ActivePools, matchID)
	return payoutTxs
}

func (bc *Blockchain) CreateGameBlock(proof GameProof, minerID string) Block {
	bc.Mutex.RLock()
	lastBlock := bc.LastBlock
	index := bc.LastBlock.Index + 1

	requiredWitnesses := int(math.Ceil(float64(proof.MaxPlayers) / 2.0))
	if len(proof.PlayerWitness) < requiredWitnesses {
		bc.Mutex.RUnlock()
		return Block{}
	}

	bc.Mutex.RUnlock()
	rewardAmount := CalculateReward(&proof)

	rewardTx := Transaction{
		ID:        fmt.Sprintf("tx_mint_%d", time.Now().UnixNano()),
		Sender:    "SYSTEM_MINT",
		Receiver:  minerID,
		Amount:    rewardAmount,
		Timestamp: time.Now().Unix(),
		Signature: "MINER_REWARD",
	}

	newBlock := Block{
		Index:         index,
		Timestamp:     time.Now().Unix(),
		Transactions:  []Transaction{rewardTx},
		GameData:      &proof,
		PrevHash:      lastBlock.Hash,
		ValidatorSigs: make(map[string]string),
	}

	newBlock.Hash = calculateHash(newBlock)
	proposal := BlockProposal{
		ProposerID: minerID,
		BlockData:  newBlock,
	}

	payload, _ := json.Marshal(proposal)
	wrapper := ValidatorMessage{Type: "PROPOSAL", Payload: payload}
	data, _ := json.Marshal(wrapper)

	if validatorTopic != nil {
		validatorTopic.Publish(ctx, data)
	}

	time.Sleep(3 * time.Second)

	sigsMutex.Lock()
	collectedSigs := pendingBlockSigs[newBlock.Hash]
	delete(pendingBlockSigs, newBlock.Hash)
	sigsMutex.Unlock()

	if collectedSigs != nil {
		newBlock.ValidatorSigs = collectedSigs
	}

	weightToAdd := bc.CalculateValidatorWeight(newBlock.Hash, newBlock.ValidatorSigs)
	newBlock.ChainWeight = lastBlock.ChainWeight + weightToAdd

	if bc.AddBlock(newBlock) {
		return newBlock
	}
	return Block{}
}

func (bc *Blockchain) CalculateValidatorWeight(blockHash string, signatures map[string]string) float64 {
	bc.Mutex.RLock()
	defer bc.Mutex.RUnlock()

	var totalWeight float64 = 0.0

	hashBytes, err := hex.DecodeString(blockHash)
	if err != nil {
		return 1.0
	}

	for peerID, sigHex := range signatures {
		pubKeyHex, exists := bc.PublicKeys[peerID]
		if !exists {
			continue
		}

		pubKeyBytes, err := hex.DecodeString(pubKeyHex)
		if err != nil {
			continue
		}

		genericPublicKey, err := x509.ParsePKIXPublicKey(pubKeyBytes)
		if err != nil {
			continue
		}

		pubKey, ok := genericPublicKey.(*ecdsa.PublicKey)
		if !ok {
			continue
		}

		sigBytes, err := hex.DecodeString(sigHex)
		if err != nil || len(sigBytes) != 64 {
			continue
		}

		r := big.NewInt(0).SetBytes(sigBytes[:32])
		s := big.NewInt(0).SetBytes(sigBytes[32:])

		isValid := ecdsa.Verify(pubKey, hashBytes, r, s)
		if !isValid {
			continue
		}

		profile, exists := bc.Profiles[peerID]
		if exists && profile.StakedEDN > 0 {
			totalWeight += profile.StakedEDN
		}
	}

	if totalWeight == 0 {
		return 1.0
	}

	return totalWeight
}

func CalculateReward(proof *GameProof) float64 {
	const BaseRatePerSecond = 0.01
	const PlayerMultiplier = 1.5

	timeReward := float64(proof.Duration) * BaseRatePerSecond
	connectionBonus := timeReward * (float64(len(proof.PlayerWitness)) * PlayerMultiplier)

	qualityFactor := float64(proof.QualityScore) / 100.0
	if proof.QualityScore < 80 {
		qualityFactor *= 0.5
	}

	return (timeReward + connectionBonus) * qualityFactor
}

func calculateHash(b Block) string {
	record := fmt.Sprintf("%d%d%v%s", b.Index, b.Timestamp, b.Transactions, b.PrevHash)
	h := sha256.New()
	h.Write([]byte(record))
	return hex.EncodeToString(h.Sum(nil))
}

func (bc *Blockchain) GetBalance(address string) float64 {
	bc.Mutex.RLock()
	defer bc.Mutex.RUnlock()
	return bc.Balances[address]
}

func GenerateKeyPair() (string, string) {
	privKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	privBytes, err := x509.MarshalECPrivateKey(privKey)
	if err != nil {
		return "", ""
	}
	privHex := hex.EncodeToString(privBytes)

	pubBytes, err := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
	if err != nil {
		return "", ""
	}
	pubHex := hex.EncodeToString(pubBytes)

	return privHex, pubHex
}

func SignTransaction(privKeyHex string, tx *Transaction) error {
	privBytes, err := hex.DecodeString(privKeyHex)
	if err != nil {
		return fmt.Errorf("invalid private key hex")
	}

	privKey, err := x509.ParseECPrivateKey(privBytes)
	if err != nil {
		return fmt.Errorf("failed to parse EC private key: %v", err)
	}

	hash := tx.GenerateTxHash()
	r, s, err := ecdsa.Sign(rand.Reader, privKey, hash)
	if err != nil {
		return err
	}

	rBytes := r.Bytes()
	sBytes := s.Bytes()
	sigBytes := make([]byte, 64)
	copy(sigBytes[32-len(rBytes):32], rBytes)
	copy(sigBytes[64-len(sBytes):64], sBytes)
	tx.Signature = hex.EncodeToString(sigBytes)
	return nil
}

func SubmitTribunalVerdict(matchID string, suspectID string, isGuilty bool) {
	payload := fmt.Sprintf("%s:%s:%t", matchID, suspectID, isGuilty)
	hashBytes := sha256.Sum256([]byte(payload))

	privBytes, _ := hex.DecodeString(myPrivKey)
	privKey, _ := x509.ParseECPrivateKey(privBytes)
	r, s, _ := ecdsa.Sign(rand.Reader, privKey, hashBytes[:])

	sigBytes := make([]byte, 64)
	copy(sigBytes[32-len(r.Bytes()):32], r.Bytes())
	copy(sigBytes[64-len(s.Bytes()):64], s.Bytes())
	sigHex := hex.EncodeToString(sigBytes)

	proposal := TribunalProposal{
		MatchID:     matchID,
		SuspectID:   suspectID,
		IsGuilty:    isGuilty,
		ValidatorID: myPeerID,
		Signature:   sigHex,
	}

	payloadBytes, _ := json.Marshal(proposal)
	wrapper := ValidatorMessage{Type: "TRIBUNAL_VERDICT", Payload: payloadBytes}
	data, _ := json.Marshal(wrapper)

	if validatorTopic != nil {
		validatorTopic.Publish(ctx, data)
	}
}
