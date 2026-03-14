/*
Eden for Unreal Engine
Copyright (C) 2026 LukaKrajina

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/
package main

/*
#include <stdlib.h>
#include <stdint.h>

typedef void (*InjectVPNPacketFn)(void* data, int len);
typedef void (*MatchFoundCallbackFn)(char* matchID, char* hostID, char* rosterList);
typedef void (*MatchEndedCallbackFn)(char* matchID);

static InjectVPNPacketFn ptrInjectVPNPacket = NULL;

static void SetInjectPacketPointer(InjectVPNPacketFn ptr) {
    ptrInjectVPNPacket = ptr;
}

static void CallInjectVPNPacket(void* data, int len) {
    if (ptrInjectVPNPacket != NULL) {
        ptrInjectVPNPacket(data, len);
    }
}

extern void HandleOutboundPacket(void* data, int len);

static void* GetGoCallback() {
    return (void*)HandleOutboundPacket;
}

static MatchFoundCallbackFn ptrMatchFoundCallback = NULL;
static MatchEndedCallbackFn ptrMatchEndedCallback = NULL;

static void SetMatchFoundCallback(MatchFoundCallbackFn ptr) {
    ptrMatchFoundCallback = ptr;
}

static void CallMatchFound(char* matchID, char* hostID, char* rosterList) {
    if (ptrMatchFoundCallback != NULL) {
        ptrMatchFoundCallback(matchID, hostID, rosterList);
    }
}

static void SetMatchEndedCallback(MatchEndedCallbackFn ptr) {
    ptrMatchEndedCallback = ptr;
}

static void CallMatchEnded(char* matchID) {
    if (ptrMatchEndedCallback != NULL) {
        ptrMatchEndedCallback(matchID);
    }
}
*/
import "C"

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	lp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
	libp2pquic "github.com/libp2p/go-libp2p/p2p/transport/quic"
	"github.com/multiformats/go-multiaddr"
	"github.com/syndtr/goleveldb/leveldb"
)

const MaxPayloadSize = 2048
const (
	FrameGame      = 0x01
	FrameHeartbeat = 0x02
)

var (
	h               host.Host
	ctx             context.Context
	nodeCancel      context.CancelFunc
	kademliaDHT     *dht.IpfsDHT
	pubSub          *pubsub.PubSub
	currentMatchID  string
	blockTopic      *pubsub.Topic
	readyFeedTopic  *pubsub.Topic
	matchFeedTopic  *pubsub.Topic
	queueTopic      *pubsub.Topic
	proposalTopic   *pubsub.Topic
	vetoTopic       *pubsub.Topic
	validatorTopic  *pubsub.Topic
	inQueue         bool
	myCurrentTicket string
	bestProposal    LobbyProposal
	proposalTimer   *time.Timer
	streamLock      sync.Mutex
	queueMutex      sync.RWMutex
	proposalMutex   sync.Mutex
	vetoMutex       sync.RWMutex
	readyMutex      sync.RWMutex
	matchesMutex    sync.RWMutex
	sigsMutex       sync.Mutex

	GlobalAppID                    = "default-eden-app"
	ProtocolID         protocol.ID = "/eden-ue5/1.0.0"
	SyncProtocolID     protocol.ID = "/eden/sync/1.0.0"
	FriendProtocolID   protocol.ID = "/eden/friend/1.0.0"
	TopicName                      = "eden-consensus-v1"
	QueueTopicName                 = "eden-queue-v1"
	ProposalTopicName              = "eden-proposals-v1"
	ValidatorTopicName             = "eden-validators-v1"
	RendezvousString               = "eden-ue5-lobby-v1.0.0"

	vpnEgressQueue   = make(chan []byte, 32768)
	matchReadyStates = make(map[string]map[string]bool)
	networkMatches   = make(map[string]MatchAnnouncement)
	activeTickets    = make(map[string]MatchmakingTicket)
	matchVetoes      = make(map[string][]string)
	pendingBlockSigs = make(map[string]map[string]string)

	bufferPool = sync.Pool{
		New: func() interface{} {
			return make([]byte, 8192)
		},
	}
)

type NetworkStats struct {
	sync.Mutex
	PacketsSent     uint64
	PacketsReceived uint64
	PacketsLost     uint64
	LastRemoteSeq   uint32
	LocalSeq        uint32
	IsInitialized   bool
}

type MatchmakingTicket struct {
	TicketID        string   `json:"ticket_id"`
	LeaderID        string   `json:"leader_id"`
	PartyMembers    []string `json:"party_members"`
	AverageElo      float64  `json:"average_elo"`
	Mode            string   `json:"mode"`
	ExpectedPlayers int      `json:"expected_players"`
	Timestamp       int64    `json:"timestamp"`
}

type LobbyProposal struct {
	ProposalID string   `json:"proposal_id"`
	HostID     string   `json:"host_id"`
	Mode       string   `json:"mode"`
	Players    []string `json:"players"`
	AverageElo float64  `json:"average_elo"`
	Timestamp  int64    `json:"timestamp"`
}

type BlockProposal struct {
	ProposerID string `json:"proposer_id"`
	BlockData  Block  `json:"block_data"`
}

type BlockSignature struct {
	ValidatorID string `json:"validator_id"`
	BlockHash   string `json:"block_hash"`
	Signature   string `json:"signature"`
}

type ValidatorMessage struct {
	Type    string `json:"type"`
	Payload []byte `json:"payload"`
}

type VetoBroadcast struct {
	MatchID string `json:"match_id"`
	PeerID  string `json:"peer_id"`
	MapName string `json:"map_name"`
}

type MatchReadyBroadcast struct {
	MatchID string `json:"match_id"`
	PeerID  string `json:"peer_id"`
}

type FriendInfo struct {
	Name       string `json:"name"`
	PeerID     string `json:"peer_id"`
	FriendCode string `json:"friend_code"`
	IsOnline   bool   `json:"is_online"`
	LastSeen   int64  `json:"last_seen"`
	Status     string `json:"status"`
}

type FriendHandshake struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Message string `json:"message"`
}

type SyncRequest struct {
	Type        string  `json:"type"`
	Index       int     `json:"index"`
	Hash        string  `json:"hash"`
	Limit       int     `json:"limit"`
	ChainWeight float64 `json:"chain_weight"`
}

type SyncResponse struct {
	Height      int     `json:"height"`
	Hash        string  `json:"hash"`
	Match       bool    `json:"match"`
	Blocks      []Block `json:"blocks"`
	ChainWeight float64 `json:"chain_weight"`
}

type MatchAnnouncement struct {
	MatchID   string  `json:"match_id"`
	HostID    string  `json:"host_id"`
	MapName   string  `json:"map_name"`
	ScoreA    int     `json:"score_a"`
	ScoreB    int     `json:"score_b"`
	Phase     string  `json:"phase"`
	Timestamp int64   `json:"timestamp"`
	TotalPool float64 `json:"total_pool"`
	TeamAPool float64 `json:"team_a_pool"`
	TeamBPool float64 `json:"team_b_pool"`
}

type StreamWorker struct {
	stream network.Stream
	queue  chan []byte
	cancel context.CancelFunc
}

var activeStreams = make(map[peer.ID]*StreamWorker)
var routingTable = make(map[string]*StreamWorker)
var rendezvousString string
var MyFriends = make(map[string]FriendInfo)
var friendMutex sync.RWMutex
var netStats NetworkStats
var lastSeenPeer time.Time
var peerMutex sync.Mutex
var friendStorePath string
var myPeerID string
var myPrivKey string
var myPubKey string
var FriendSystemKey = []byte("0123456789ABCDEF0123456789ABCDEF")

func FastStreamWorker(ctx context.Context, s network.Stream) *StreamWorker {
	ctx, cancel := context.WithCancel(ctx)
	sw := &StreamWorker{
		stream: s,
		queue:  make(chan []byte, 4096),
		cancel: cancel,
	}
	go sw.writerLoop(ctx)
	return sw
}

func (sw *StreamWorker) writerLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-sw.queue:
			_, err := sw.stream.Write(frame)
			if err != nil {
				sw.Close()
				return
			}
		}
	}
}

func (sw *StreamWorker) Enqueue(frame []byte) {
	frameCopy := make([]byte, len(frame))
	copy(frameCopy, frame)
	select {
	case sw.queue <- frameCopy:
	default:
		netStats.Lock()
		netStats.PacketsLost++
		netStats.Unlock()
	}
}

func (sw *StreamWorker) Close() {
	sw.cancel()
	sw.stream.Close()
}

//export UpdateMyProfile
func UpdateMyProfile(username *C.char, avatarURL *C.char) *C.char {
	user := C.GoString(username)
	url := C.GoString(avatarURL)
	payload := fmt.Sprintf("%s|%s", user, url)

	pubKeyBytes, err := hex.DecodeString(myPubKey)
	if err != nil {
		return C.CString("Error: Invalid Public Key")
	}

	tx := Transaction{
		ID:        fmt.Sprintf("prof_%d", time.Now().UnixNano()),
		Type:      TxTypeUpdateProfile,
		Sender:    h.ID().String(),
		Receiver:  "IDENTITY_CONTRACT",
		Amount:    0,
		Payload:   payload,
		Timestamp: time.Now().Unix(),
		PublicKey: pubKeyBytes,
		Nonce:     GetNextNonce(h.ID().String()),
	}

	if err := SignTransaction(myPrivKey, &tx); err != nil {
		return C.CString("Error: Signing Failed")
	}

	EdenChain.Mutex.RLock()
	lastIndex := EdenChain.LastBlock.Index
	prevHash := EdenChain.LastBlock.Hash
	EdenChain.Mutex.RUnlock()

	newBlock := Block{
		Index:        lastIndex + 1,
		Timestamp:    time.Now().Unix(),
		Transactions: []Transaction{tx},
		PrevHash:     prevHash,
	}
	newBlock.Hash = calculateHash(newBlock)

	if EdenChain.AddBlock(newBlock) {
		broadcastBlock(newBlock)
		return C.CString("Success")
	}
	return C.CString("Error: Block Rejected")
}

//export GetPeerProfile
func GetPeerProfile(peerID *C.char) *C.char {
	pid := C.GoString(peerID)
	if pid == "" {
		pid = h.ID().String()
	}
	EdenChain.Mutex.RLock()
	profile := EdenChain.GetOrInitProfile(pid)
	EdenChain.Mutex.RUnlock()

	data, _ := json.Marshal(profile)
	return C.CString(string(data))
}

func StartVPNEgressWorker() {
	go func() {
		for frame := range vpnEgressQueue {
			if len(frame) < 27 {
				continue
			}
			destIP := fmt.Sprintf("%d.%d.%d.%d", frame[7+16], frame[7+17], frame[7+18], frame[7+19])
			streamLock.Lock()
			targetWorker, isUnicast := routingTable[destIP]
			if isUnicast {
				streamLock.Unlock()
				targetWorker.Enqueue(frame)
			} else if destIP == "255.255.255.255" || strings.HasSuffix(destIP, ".255") {
				streamsCopy := make([]*StreamWorker, 0, len(activeStreams))
				for _, sw := range activeStreams {
					streamsCopy = append(streamsCopy, sw)
				}
				streamLock.Unlock()
				for _, sw := range streamsCopy {
					sw.Enqueue(frame)
				}
			} else {
				streamLock.Unlock()
			}
		}
	}()
}

func EncryptFriendCode(peerID string) string {
	block, err := aes.NewCipher(FriendSystemKey)
	if err != nil {
		panic(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	hash := sha256.Sum256([]byte(peerID))
	nonceSize := gcm.NonceSize()
	nonce := make([]byte, nonceSize)
	copy(nonce, hash[:nonceSize])
	ciphertext := gcm.Seal(nonce, nonce, []byte(peerID), nil)
	return base64.URLEncoding.EncodeToString(ciphertext)
}

func DecryptFriendCode(code string) (string, error) {
	data, err := base64.URLEncoding.DecodeString(code)
	if err != nil {
		return "", err
	}
	block, _ := aes.NewCipher(FriendSystemKey)
	gcm, _ := cipher.NewGCM(block)
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("invalid code")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func GetNextNonce(sender string) uint64 {
	EdenChain.Mutex.RLock()
	defer EdenChain.Mutex.RUnlock()
	return EdenChain.AccountNonces[sender] + 1
}

//export GenerateAndRegisterFriendCode
func GenerateAndRegisterFriendCode() *C.char {
	code := EncryptFriendCode(h.ID().String())
	pubKeyBytes, err := hex.DecodeString(myPubKey)
	if err != nil {
		return C.CString("Error: Invalid Public Key")
	}

	tx := Transaction{
		ID:        fmt.Sprintf("reg_%d", time.Now().UnixNano()),
		Type:      TxTypeRegisterFriend,
		Sender:    h.ID().String(),
		Receiver:  "FRIEND_REGISTRY",
		Amount:    0,
		Payload:   code,
		Timestamp: time.Now().Unix(),
		PublicKey: pubKeyBytes,
		Nonce:     GetNextNonce(h.ID().String()),
	}
	SignTransaction(myPrivKey, &tx)

	EdenChain.Mutex.RLock()
	lastIndex := EdenChain.LastBlock.Index
	prevHash := EdenChain.LastBlock.Hash
	EdenChain.Mutex.RUnlock()

	newBlock := Block{
		Index:        lastIndex + 1,
		Timestamp:    time.Now().Unix(),
		Transactions: []Transaction{tx},
		PrevHash:     prevHash,
	}
	newBlock.Hash = calculateHash(newBlock)

	if EdenChain.AddBlock(newBlock) {
		broadcastBlock(newBlock)
		return C.CString(code)
	}
	return C.CString("Error: Chain Rejected")
}

//export AddFriendByCode
func AddFriendByCode(code *C.char) *C.char {
	cStr := C.GoString(code)
	peerID, err := DecryptFriendCode(cStr)
	if err != nil {
		return C.CString("Error: Invalid Code")
	}
	EdenChain.Mutex.RLock()
	registeredOwner, exists := EdenChain.FriendRegistry[cStr]
	EdenChain.Mutex.RUnlock()

	if !exists {
		return C.CString("Error: Friend Code not found on blockchain.")
	}
	if registeredOwner != peerID {
		return C.CString("Error: Security Mismatch")
	}

	friendMutex.Lock()
	MyFriends[peerID] = FriendInfo{
		Name:       "Pending Peer",
		PeerID:     peerID,
		FriendCode: cStr,
		IsOnline:   false,
		Status:     "Pending_Sent",
	}
	friendMutex.Unlock()
	SaveFriends()
	go SendFriendSignal(peerID, "REQUEST")
	return C.CString("Success: " + peerID)
}

//export FetchFriendList
func FetchFriendList() *C.char {
	friendMutex.Lock()
	defer friendMutex.Unlock()
	var list []FriendInfo
	for id, info := range MyFriends {
		if len(h.Peerstore().Addrs(peer.ID(id))) > 0 {
			info.IsOnline = true
		} else {
			info.IsOnline = false
		}
		list = append(list, info)
	}
	data, _ := json.Marshal(list)
	return C.CString(string(data))
}

//export GetNetworkMatches
func GetNetworkMatches() *C.char {
	matchesMutex.Lock()
	var active []MatchAnnouncement
	now := time.Now().Unix()
	for id, m := range networkMatches {
		if now-m.Timestamp < 60 {
			active = append(active, m)
		} else {
			delete(networkMatches, id)
		}
	}
	matchesMutex.Unlock()

	EdenChain.Mutex.RLock()
	for mID, session := range EdenChain.MatchSessions {
		found := false
		for _, existing := range active {
			if existing.MatchID == mID {
				found = true
				break
			}
		}
		if !found {
			var total, a, b float64
			if pool, ok := EdenChain.ActivePools[mID]; ok {
				total = pool.TotalPool
				a = pool.TeamAPool
				b = pool.TeamBPool
			}
			active = append(active, MatchAnnouncement{
				MatchID:   mID,
				HostID:    session.HostID,
				Phase:     "lobby",
				Timestamp: session.StartTime,
				TotalPool: total,
				TeamAPool: a,
				TeamBPool: b,
			})
		}
	}
	EdenChain.Mutex.RUnlock()

	data, _ := json.Marshal(active)
	return C.CString(string(data))
}

func writeFrame(s network.Stream, data []byte) error {
	length := uint32(len(data))
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, length)
	if _, err := s.Write(lenBuf); err != nil {
		return err
	}
	if _, err := s.Write(data); err != nil {
		return err
	}
	return nil
}

func readFrame(s network.Stream) ([]byte, error) {
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(s, lenBuf); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(lenBuf)
	if length > 10*1024*1024 {
		return nil, fmt.Errorf("message too large: %d bytes", length)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(s, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

//export StartEdenNode
func StartEdenNode(virtualIP *C.char, gameID *C.char) *C.char {
	ctx, nodeCancel = context.WithCancel(context.Background())
	appID := C.GoString(gameID)
	if appID != "" {
		GlobalAppID = appID
	}

	ProtocolID = protocol.ID(fmt.Sprintf("/%s/game/1.0.0", GlobalAppID))
	SyncProtocolID = protocol.ID(fmt.Sprintf("/%s/sync/1.0.0", GlobalAppID))
	FriendProtocolID = protocol.ID(fmt.Sprintf("/%s/friend/1.0.0", GlobalAppID))

	TopicName = fmt.Sprintf("%s-consensus", GlobalAppID)
	QueueTopicName = fmt.Sprintf("%s-queue", GlobalAppID)
	ProposalTopicName = fmt.Sprintf("%s-proposals", GlobalAppID)
	ValidatorTopicName = fmt.Sprintf("%s-validators", GlobalAppID)
	RendezvousString = fmt.Sprintf("%s-lobby", GlobalAppID)

	InitializeWallet()

	privBytes, err := hex.DecodeString(myPrivKey)
	if err != nil {
		return C.CString("Error: Invalid Private Key Hex")
	}

	libp2pKey, err := lp2pcrypto.UnmarshalECDSAPrivateKey(privBytes)
	if err != nil {
		return C.CString("Error: Failed to convert to Libp2p Key: " + err.Error())
	}

	bootstrapPeers := []string{
		"/dnsaddr/bootstrap.libp2p.io/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN",
		"/dnsaddr/bootstrap.libp2p.io/p2p/QmQCU2EcMqAqQPR2i9bChDtGNJchTbq5TbXJJ16u19uLTa",
		"/dnsaddr/bootstrap.libp2p.io/p2p/QmbLHAnMoJPWSCR5Zhtx6BHJX9KiKNN6tpvbUcqanj75Nb",
		"/dnsaddr/bootstrap.libp2p.io/p2p/QmcZf59bWwK5XFi76CZX8cbJ4BhTzzA3gU1ZjYZcYW3dwt",
	}

	var staticRelays []peer.AddrInfo
	for _, peerAddr := range bootstrapPeers {
		addr, err := multiaddr.NewMultiaddr(peerAddr)
		if err != nil {
			if peerInfo, err := peer.AddrInfoFromP2pAddr(addr); err == nil {
				staticRelays = append(staticRelays, *peerInfo)
			}
		}
	}

	h, err = libp2p.New(
		libp2p.Identity(libp2pKey),
		libp2p.EnableAutoNATv2(),
		libp2p.EnableHolePunching(),
		libp2p.Transport(libp2pquic.NewTransport),
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/udp/0/quic-v1"),
		libp2p.EnableRelay(),
		libp2p.EnableAutoRelayWithStaticRelays(staticRelays),
	)

	if err != nil {
		return C.CString("Error: " + err.Error())
	}

	InitializeChain(fmt.Sprintf("./%s_db_%s", GlobalAppID, h.ID().String()))

	h.SetStreamHandler(SyncProtocolID, HandleSyncRequest)
	h.SetStreamHandler(FriendProtocolID, HandleFriendStream)
	h.SetStreamHandler(ProtocolID, func(s network.Stream) {
		fmt.Println("[P2P] Incoming Game Connection")
		if oldWorker, exists := activeStreams[s.Conn().RemotePeer()]; exists {
			oldWorker.Close()
		}
		sw := FastStreamWorker(ctx, s)
		streamLock.Lock()
		activeStreams[s.Conn().RemotePeer()] = sw
		routingTable[getIPFromPeerID(s.Conn().RemotePeer().String())] = sw
		streamLock.Unlock()
		go readStreamLoop(s)
	})

	kademliaDHT, _ = dht.New(ctx, h)
	kademliaDHT.Bootstrap(ctx)

	var wg sync.WaitGroup
	for _, p := range staticRelays {
		wg.Add(1)
		go func(pInfo peer.AddrInfo) {
			defer wg.Done()
			h.Connect(ctx, pInfo)
		}(p)
	}
	wg.Wait()

	routingDiscovery := routing.NewRoutingDiscovery(kademliaDHT)
	dutil.Advertise(ctx, routingDiscovery, rendezvousString)

	setupPubSub()
	LoadFriends()
	StartVPNEgressWorker()

	myPeerID = h.ID().String()
	fmt.Printf("[%s] Node Started. ID: %s\n", GlobalAppID, h.ID().String())

	return C.CString(getIPFromPeerID(h.ID().String()))
}

func HandleFriendStream(s network.Stream) {
	defer s.Close()
	buf, err := readFrame(s)
	if err != nil {
		return
	}
	var msg FriendHandshake
	json.Unmarshal(buf, &msg)
	remotePID := s.Conn().RemotePeer().String()

	friendMutex.Lock()
	entry, exists := MyFriends[remotePID]
	switch msg.Type {
	case "REQUEST":
		if !exists {
			MyFriends[remotePID] = FriendInfo{
				Name:     msg.Name,
				PeerID:   remotePID,
				Status:   "Pending_Received",
				LastSeen: time.Now().Unix(),
			}
		}
	case "ACCEPT":
		if exists {
			entry.Status = "Confirmed"
			entry.Name = msg.Name
			MyFriends[remotePID] = entry
		}
	case "REJECT":
		if exists {
			delete(MyFriends, remotePID)
		}
	}
	friendMutex.Unlock()
	SaveFriends()
}

func HandleSyncRequest(s network.Stream) {
	defer s.Close()
	buf, err := readFrame(s)
	if err != nil {
		return
	}

	var req SyncRequest
	json.Unmarshal(buf, &req)

	EdenChain.Mutex.RLock()
	localHeight := EdenChain.LastBlock.Index + 1
	localWeight := EdenChain.LastBlock.ChainWeight

	resp := SyncResponse{
		Height:      localHeight,
		ChainWeight: localWeight,
	}

	switch req.Type {
	case "STATUS":
		if localHeight > 0 {
			resp.Hash = EdenChain.LastBlock.Hash
		}
	case "VERIFY":
		blocks := EdenChain.GetBlocksRange(req.Index, req.Index+1)
		if len(blocks) > 0 {
			localHash := blocks[0].Hash
			resp.Hash = localHash
			resp.Match = (localHash == req.Hash)
		} else {
			resp.Match = false
		}
	case "BLOCKS":
		if req.Index < localHeight {
			end := req.Index + req.Limit
			if end > localHeight {
				end = localHeight
			}
			resp.Blocks = EdenChain.GetBlocksRange(req.Index, end)
		}
	}
	EdenChain.Mutex.RUnlock()

	data, _ := json.Marshal(resp)
	writeFrame(s, data)
}

func (bc *Blockchain) GetBlocksRange(start, end int) []Block {
	var blocks []Block
	for i := start; i < end; i++ {
		key := fmt.Sprintf("block_%d", i)
		data, err := bc.Database.Get([]byte(key), nil)
		if err == nil {
			var b Block
			json.Unmarshal(data, &b)
			blocks = append(blocks, b)
		}
	}
	return blocks
}

func TriggerSync(pID peer.ID) {
	status, err := requestSync(pID, SyncRequest{Type: "STATUS"})
	if err != nil {
		return
	}

	EdenChain.Mutex.RLock()
	localHeight := EdenChain.LastBlock.Index + 1
	localWeight := EdenChain.LastBlock.ChainWeight
	EdenChain.Mutex.RUnlock()

	if status.ChainWeight <= localWeight || status.Height == 0 {
		return
	}

	low := 0
	high := localHeight - 1
	if status.Height-1 < high {
		high = status.Height - 1
	}

	ancestor := -1
	if high >= 0 {
		tipBlocks := EdenChain.GetBlocksRange(high, high+1)
		if len(tipBlocks) > 0 {
			tipCheck, _ := requestSync(pID, SyncRequest{
				Type:  "VERIFY",
				Index: high,
				Hash:  tipBlocks[0].Hash,
			})
			if tipCheck.Match {
				ancestor = high
			}
		}
	}

	if ancestor == -1 && localHeight > 0 {
		for low <= high {
			mid := low + (high-low)/2
			midBlocks := EdenChain.GetBlocksRange(mid, mid+1)
			if len(midBlocks) == 0 {
				break
			}
			verifyResp, err := requestSync(pID, SyncRequest{
				Type:  "VERIFY",
				Index: mid,
				Hash:  midBlocks[0].Hash,
			})
			if err != nil {
				break
			}
			if verifyResp.Match {
				ancestor = mid
				low = mid + 1
			} else {
				high = mid - 1
			}
		}
	}

	startDownload := ancestor + 1
	var pendingBlocks []Block
	for startDownload < status.Height {
		req := SyncRequest{
			Type:  "BLOCKS",
			Index: startDownload,
			Limit: 100,
		}
		resp, err := requestSync(pID, req)
		if err != nil || len(resp.Blocks) == 0 {
			break
		}
		pendingBlocks = append(pendingBlocks, resp.Blocks...)
		startDownload += len(resp.Blocks)
	}

	if len(pendingBlocks) == 0 || pendingBlocks[len(pendingBlocks)-1].ChainWeight <= localWeight {
		return
	}

	EdenChain.Mutex.Lock()
	batch := new(leveldb.Batch)
	for i := localHeight - 1; i > ancestor; i-- {
		key := fmt.Sprintf("block_%d", i)
		batch.Delete([]byte(key))
	}

	batch.Put([]byte("latest_index"), []byte(fmt.Sprintf("%d", ancestor)))
	EdenChain.Database.Write(batch, nil)
	EdenChain.Balances = make(map[string]float64)
	EdenChain.Profiles = make(map[string]*UserProfile)
	EdenChain.ActivePools = make(map[string]*BettingPool)
	EdenChain.AccountNonces = make(map[string]uint64)
	EdenChain.MatchSessions = make(map[string]MatchSessionInfo)
	EdenChain.MatchVotes = make(map[string]map[string]string)
	EdenChain.FriendRegistry = make(map[string]string)
	EdenChain.PublicKeys = make(map[string]string)
	EdenChain.TribunalVotes = make(map[string]map[string]map[string]bool)
	EdenChain.QueueBans = make(map[string]int64)

	validBlocks := EdenChain.GetBlocksRange(0, ancestor+1)
	for _, b := range validBlocks {
		EdenChain.ProcessBlockState(b)
	}
	if len(validBlocks) > 0 {
		EdenChain.LastBlock = validBlocks[len(validBlocks)-1]
	} else {
		EdenChain.LastBlock = Block{Index: -1, Hash: "0"}
	}
	for _, b := range pendingBlocks {
		if !EdenChain.ProcessBlockState(b) {
			break
		}
		EdenChain.LastBlock = b
		EdenChain.SaveBlockToDB(b)
	}
	EdenChain.Mutex.Unlock()
}

func requestSync(pID peer.ID, req SyncRequest) (SyncResponse, error) {
	s, err := h.NewStream(ctx, pID, SyncProtocolID)
	if err != nil {
		return SyncResponse{}, err
	}
	defer s.Close()
	reqData, _ := json.Marshal(req)
	if err := writeFrame(s, reqData); err != nil {
		return SyncResponse{}, err
	}
	buf, err := readFrame(s)
	if err != nil {
		return SyncResponse{}, err
	}
	var resp SyncResponse
	err = json.Unmarshal(buf, &resp)
	return resp, err
}

//export StartMatch
func StartMatch(matchID *C.char, playerList *C.char, password *C.char) *C.char {
	mID := C.GoString(matchID)
	rosterStr := C.GoString(playerList)
	serverPwd := C.GoString(password)

	currentMatchID = mID
	roster := strings.Split(rosterStr, ",")
	EdenChain.Mutex.RLock()
	var encryptedPasswords []string
	for _, peerID := range roster {
		peerPubKey, exists := EdenChain.PublicKeys[peerID]
		if !exists || peerID == h.ID().String() {
			encryptedPasswords = append(encryptedPasswords, "HOST")
			continue
		}
		aesKey, err := DeriveSharedAESKey(myPrivKey, peerPubKey)
		if err == nil {
			enc := EncryptPassword(aesKey, serverPwd)
			encryptedPasswords = append(encryptedPasswords, enc)
		} else {
			encryptedPasswords = append(encryptedPasswords, "ERR")
		}
	}
	EdenChain.Mutex.RUnlock()

	encPwdStr := strings.Join(encryptedPasswords, ",")
	finalPayload := fmt.Sprintf("%s|%s|%s", mID, rosterStr, encPwdStr)
	pubKeyBytes, err := hex.DecodeString(myPubKey)
	if err != nil {
		return C.CString("Error: Invalid Public Key")
	}

	tx := Transaction{
		ID:        fmt.Sprintf("init_%s_%d", mID, time.Now().UnixNano()),
		Type:      "MATCH_START",
		Sender:    h.ID().String(),
		Receiver:  "CONSENSUS_ENGINE",
		Amount:    0,
		Payload:   finalPayload,
		Timestamp: time.Now().Unix(),
		PublicKey: pubKeyBytes,
		Nonce:     GetNextNonce(h.ID().String()),
	}

	if !strings.Contains(rosterStr, h.ID().String()) {
		return C.CString("Error: Invalid Roster")
	}

	if err := SignTransaction(myPrivKey, &tx); err != nil {
		return C.CString("Error: Signing Failed")
	}

	EdenChain.Mutex.RLock()
	lastIndex := EdenChain.LastBlock.Index
	prevHash := EdenChain.LastBlock.Hash
	EdenChain.Mutex.RUnlock()

	newBlock := Block{
		Index:        lastIndex + 1,
		Timestamp:    time.Now().Unix(),
		Transactions: []Transaction{tx},
		PrevHash:     prevHash,
	}
	newBlock.Hash = calculateHash(newBlock)

	if EdenChain.AddBlock(newBlock) {
		broadcastBlock(newBlock)
		return C.CString("Success")
	}

	return C.CString("Error: Block Rejected")
}

//export GetMatchPassword
func GetMatchPassword(matchID *C.char) *C.char {
	mID := C.GoString(matchID)
	myID := h.ID().String()

	EdenChain.Mutex.RLock()
	defer EdenChain.Mutex.RUnlock()

	for i := EdenChain.LastBlock.Index; i >= 0; i-- {
		key := fmt.Sprintf("block_%d", i)
		data, err := EdenChain.Database.Get([]byte(key), nil)
		if err != nil {
			continue
		}

		var b Block
		json.Unmarshal(data, &b)

		for _, tx := range b.Transactions {
			if tx.Type == "MATCH_START" && strings.HasPrefix(tx.Payload, mID+"|") {
				parts := strings.Split(tx.Payload, "|")
				if len(parts) != 3 {
					return C.CString("")
				}

				roster := strings.Split(parts[1], ",")
				encPasswords := strings.Split(parts[2], ",")

				for idx, playerID := range roster {
					if playerID == myID && idx < len(encPasswords) {
						if encPasswords[idx] == "HOST" {
							return C.CString("")
						}

						hostPubKey := EdenChain.PublicKeys[tx.Sender]
						aesKey, err := DeriveSharedAESKey(myPrivKey, hostPubKey)
						if err != nil {
							return C.CString("Error: Crypto Failure")
						}

						plaintext := DecryptPassword(aesKey, encPasswords[idx])
						return C.CString(plaintext)
					}
				}
			}
		}
	}
	return C.CString("Error: Match Not Found")
}

//export StopEdenNode
func StopEdenNode() {
	streamLock.Lock()
	for _, sw := range activeStreams {
		if sw != nil {
			sw.Close()
		}
	}
	activeStreams = make(map[peer.ID]*StreamWorker)
	streamLock.Unlock()
	if nodeCancel != nil {
		nodeCancel()
	}
	if h != nil {
		h.Close()
	}
}

//export AbortMatch
func AbortMatch(matchID *C.char) *C.char {
	mID := C.GoString(matchID)
	pubKeyBytes, _ := hex.DecodeString(myPubKey)

	tx := Transaction{
		ID:        fmt.Sprintf("abort_%s_%d", mID, time.Now().UnixNano()),
		Type:      "MATCH_ABORT",
		Sender:    h.ID().String(),
		Receiver:  "CONSENSUS_ENGINE",
		Amount:    0,
		Payload:   mID,
		Timestamp: time.Now().Unix(),
		PublicKey: pubKeyBytes,
		Nonce:     GetNextNonce(h.ID().String()),
	}

	if err := SignTransaction(myPrivKey, &tx); err != nil {
		return C.CString("Error: Signing Failed")
	}

	newBlock := Block{
		Index:        EdenChain.LastBlock.Index + 1,
		Timestamp:    time.Now().Unix(),
		Transactions: []Transaction{tx},
		PrevHash:     EdenChain.LastBlock.Hash,
	}
	newBlock.Hash = calculateHash(newBlock)

	if EdenChain.AddBlock(newBlock) {
		broadcastBlock(newBlock)
		return C.CString("Success")
	}
	return C.CString("Error: Block Rejected")
}

//export SubmitGameBlock
func SubmitGameBlock(duration C.int, playerCount C.int) *C.char {
	peerMutex.Lock()
	alive := time.Since(lastSeenPeer) < 15*time.Second
	peerMutex.Unlock()
	if !alive {
		return C.CString("Error: Node Offline")
	}

	proof := GameProof{
		MatchID:      fmt.Sprintf("match_%d", time.Now().Unix()),
		Duration:     int(duration),
		MaxPlayers:   int(playerCount),
		QualityScore: GetNetworkQuality(),
	}

	newBlock := EdenChain.CreateGameBlock(proof, h.ID().String())
	if newBlock.Hash == "" {
		return C.CString("Error: Block Creation Failed")
	}

	broadcastBlock(newBlock)
	return C.CString(newBlock.Hash)
}

func InitializeWallet() {
	const KeyFileName = "eden_identity.key"
	if _, err := os.Stat(KeyFileName); err == nil {
		keyBytes, err := os.ReadFile(KeyFileName)
		if err == nil {
			savedPrivKeyHex := string(keyBytes)
			privBytes, err := hex.DecodeString(savedPrivKeyHex)
			if err == nil {
				privKey, err := x509.ParseECPrivateKey(privBytes)
				if err == nil {
					pubBytes, _ := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
					myPrivKey = savedPrivKeyHex
					myPubKey = hex.EncodeToString(pubBytes)
					return
				}
			}
		}
	}

	myPrivKey, myPubKey = GenerateKeyPair()
	os.WriteFile(KeyFileName, []byte(myPrivKey), 0600)
}

//export GetWalletPubKey
func GetWalletPubKey() *C.char {
	return C.CString(myPubKey)
}

//export GetWalletBalance
func GetWalletBalance(address *C.char) C.double {
	return C.double(EdenChain.GetBalance(C.GoString(address)))
}

//export PlaceBet
func PlaceBet(matchID *C.char, team *C.char, amount C.double) *C.char {
	mID := C.GoString(matchID)
	tm := C.GoString(team)
	amt := float64(amount)

	pubKeyBytes, err := hex.DecodeString(myPubKey)
	if err != nil {
		return C.CString("Error: Invalid Public Key")
	}

	tx := Transaction{
		ID:        fmt.Sprintf("bet_%d", time.Now().UnixNano()),
		Type:      TxTypeBet,
		Sender:    h.ID().String(),
		Receiver:  "POOL_CONTRACT",
		Amount:    amt,
		Payload:   fmt.Sprintf("%s:%s", mID, tm),
		Timestamp: time.Now().Unix(),
		PublicKey: pubKeyBytes,
		Nonce:     GetNextNonce(h.ID().String()),
	}

	if err := SignTransaction(myPrivKey, &tx); err != nil {
		return C.CString("Error: Signing Failed")
	}

	newBlock := Block{
		Index:        EdenChain.LastBlock.Index + 1,
		Timestamp:    time.Now().Unix(),
		Transactions: []Transaction{tx},
		PrevHash:     EdenChain.LastBlock.Hash,
	}
	newBlock.Hash = calculateHash(newBlock)

	if EdenChain.AddBlock(newBlock) {
		broadcastBlock(newBlock)
		return C.CString(tx.ID)
	}
	return C.CString("Error: Bet Failed")
}

//export SendTransaction
func SendTransaction(receiver *C.char, amount C.double) C.int {
	r := C.GoString(receiver)
	amt := float64(amount)

	pubKeyBytes, err := hex.DecodeString(myPubKey)
	if err != nil {
		return 0
	}

	tx := Transaction{
		ID:        fmt.Sprintf("tx_%d", time.Now().UnixNano()),
		Type:      TxTypeTransfer,
		Sender:    h.ID().String(),
		Receiver:  r,
		Amount:    amt,
		Payload:   "",
		Timestamp: time.Now().Unix(),
		PublicKey: pubKeyBytes,
		Nonce:     GetNextNonce(h.ID().String()),
	}

	err = SignTransaction(myPrivKey, &tx)
	if err != nil {
		return 0
	}

	newBlock := Block{
		Index:        EdenChain.LastBlock.Index + 1,
		Timestamp:    time.Now().Unix(),
		Transactions: []Transaction{tx},
		PrevHash:     EdenChain.LastBlock.Hash,
	}
	newBlock.Hash = calculateHash(newBlock)

	if EdenChain.AddBlock(newBlock) {
		broadcastBlock(newBlock)
		return 1
	}
	return 0
}

func SendFriendSignal(peerIDStr string, signalType string) {
	pID, err := peer.Decode(peerIDStr)
	if err != nil {
		return
	}

	if h.Network().Connectedness(pID) != network.Connected {
		peerInfo, err := kademliaDHT.FindPeer(ctx, pID)
		if err == nil {
			h.Connect(ctx, peerInfo)
		}
	}

	s, err := h.NewStream(ctx, pID, FriendProtocolID)
	if err != nil {
		return
	}
	defer s.Close()

	myID := h.ID().String()
	myName := "Unknown User"

	EdenChain.Mutex.RLock()
	if profile, exists := EdenChain.Profiles[myID]; exists {
		myName = profile.Username
	} else {
		myName = GenerateFixedUsername(myID)
	}
	EdenChain.Mutex.RUnlock()

	payload := FriendHandshake{
		Type:    signalType,
		Name:    myName,
		Message: "",
	}

	data, _ := json.Marshal(payload)
	writeFrame(s, data)
}

func getFriendFilePath() string {
	if friendStorePath != "" {
		return friendStorePath
	}
	return fmt.Sprintf("friends_%s.json", h.ID().String())
}

func SaveFriends() {
	friendMutex.Lock()
	defer friendMutex.Unlock()

	data, err := json.MarshalIndent(MyFriends, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(getFriendFilePath(), data, 0644)
}

func LoadFriends() {
	friendMutex.Lock()
	defer friendMutex.Unlock()

	path := getFriendFilePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	json.Unmarshal(data, &MyFriends)
}

//export InitPacketBridge
func InitPacketBridge(fn C.InjectVPNPacketFn) {
	C.SetInjectPacketPointer(fn)
}

//export HandleOutboundPacket
func HandleOutboundPacket(data unsafe.Pointer, length C.int) {
	streamLock.Lock()
	streams := make([]*StreamWorker, 0, len(activeStreams))
	for _, sw := range activeStreams {
		streams = append(streams, sw)
	}
	streamLock.Unlock()

	if len(streams) == 0 {
		return
	}

	payloadLen := int(length)

	if payloadLen <= 0 || payloadLen > MaxPayloadSize {
		return
	}

	totalLen := 7 + payloadLen
	bufPtr := bufferPool.Get().([]byte)
	if cap(bufPtr) < totalLen {
		bufPtr = make([]byte, totalLen)
	}
	frame := bufPtr[:totalLen]

	netStats.Lock()
	netStats.LocalSeq++
	seq := netStats.LocalSeq
	netStats.PacketsSent++
	netStats.Unlock()

	frame[0] = FrameGame
	frame[1] = byte(uint16(payloadLen) >> 8)
	frame[2] = byte(uint16(payloadLen))
	frame[3] = byte(seq >> 24)
	frame[4] = byte(seq >> 16)
	frame[5] = byte(seq >> 8)
	frame[6] = byte(seq)

	cData := unsafe.Slice((*byte)(data), payloadLen)
	egressFrame := make([]byte, totalLen)
	copy(frame[7:], cData)
	copy(egressFrame, frame)

	select {
	case vpnEgressQueue <- egressFrame:
	default:
		netStats.Lock()
		netStats.PacketsLost++
		netStats.Unlock()
	}

	for _, sw := range streams {
		sw.Enqueue(frame)
	}

	bufferPool.Put(bufPtr)
}

func readStreamLoop(s network.Stream) {
	defer s.Close()
	defer func() {
		remotePeer := s.Conn().RemotePeer()
		destIP := getIPFromPeerID(remotePeer.String())

		streamLock.Lock()
		if sw, ok := activeStreams[remotePeer]; ok {
			sw.cancel()
			delete(activeStreams, remotePeer)
		}
		delete(routingTable, destIP)
		streamLock.Unlock()
	}()

	header := make([]byte, 7)

	for {
		if _, err := io.ReadFull(s, header); err != nil {
			return
		}

		payloadLen := uint16(header[1])<<8 | uint16(header[2])
		remoteSeq := uint32(header[3])<<24 | uint32(header[4])<<16 | uint32(header[5])<<8 | uint32(header[6])

		updateNetworkStats(remoteSeq)

		if header[0] == FrameHeartbeat {
			continue
		}

		if payloadLen > 0 {
			bufPtr := bufferPool.Get().([]byte)
			if cap(bufPtr) < int(payloadLen) {
				bufPtr = make([]byte, int(payloadLen))
			}
			payload := bufPtr[:payloadLen]

			if _, err := io.ReadFull(s, payload); err != nil {
				bufferPool.Put(bufPtr)
				return
			}

			cPtr := C.CBytes(payload)
			C.CallInjectVPNPacket(cPtr, C.int(payloadLen))
			C.free(cPtr)

			bufferPool.Put(bufPtr)
		}

		if payloadLen > MaxPayloadSize {
			s.Close()
			return
		}
	}
}

func updateNetworkStats(remoteSeq uint32) {
	netStats.Lock()
	defer netStats.Unlock()

	if !netStats.IsInitialized {
		netStats.IsInitialized = true
	} else if remoteSeq > netStats.LastRemoteSeq+1 {
		netStats.PacketsLost += uint64(remoteSeq - netStats.LastRemoteSeq - 1)
	}

	netStats.LastRemoteSeq = remoteSeq
	netStats.PacketsReceived++

	peerMutex.Lock()
	lastSeenPeer = time.Now()
	peerMutex.Unlock()
}

func GetNetworkQuality() int {
	netStats.Lock()
	defer netStats.Unlock()
	total := netStats.PacketsReceived + netStats.PacketsLost
	if total == 0 {
		return 100
	}
	lossRate := float64(netStats.PacketsLost) / float64(total)
	score := 100 - int(lossRate*100)
	if score < 0 {
		return 0
	}
	return score
}

func setupPubSub() {
	var err error
	pubSub, err = pubsub.NewGossipSub(
		ctx,
		h,
		pubsub.WithFloodPublish(true),
		pubsub.WithPeerExchange(true),
	)
	if err != nil {
		return
	}

	blockTopic, err = pubSub.Join(TopicName)
	if err != nil {
		return
	}

	sub, _ := blockTopic.Subscribe()
	go func() {
		for {
			msg, err := sub.Next(ctx)
			if err != nil {
				return
			}
			if msg.ReceivedFrom == h.ID() {
				continue
			}

			var b Block
			if err := json.Unmarshal(msg.Data, &b); err == nil {
				EdenChain.AddBlock(b)
			}
		}
	}()

	vetoTopic, err = pubSub.Join("eden-match-vetoes")
	if err == nil {
		vetoSub, _ := vetoTopic.Subscribe()
		go func() {
			for {
				msg, err := vetoSub.Next(ctx)
				if err != nil {
					return
				}

				var v VetoBroadcast
				if err := json.Unmarshal(msg.Data, &v); err == nil {
					vetoMutex.Lock()
					alreadyBanned := false
					for _, m := range matchVetoes[v.MatchID] {
						if m == v.MapName {
							alreadyBanned = true
							break
						}
					}
					if !alreadyBanned {
						matchVetoes[v.MatchID] = append(matchVetoes[v.MatchID], v.MapName)
					}
					vetoMutex.Unlock()
				}
			}
		}()
	}

	readyFeedTopic, _ = pubSub.Join("eden-match-ready")
	readySub, _ := readyFeedTopic.Subscribe()
	go func() {
		for {
			msg, err := readySub.Next(ctx)
			if err != nil {
				return
			}

			var ann MatchReadyBroadcast
			if err := json.Unmarshal(msg.Data, &ann); err == nil {
				readyMutex.Lock()
				if matchReadyStates[ann.MatchID] == nil {
					matchReadyStates[ann.MatchID] = make(map[string]bool)
				}
				matchReadyStates[ann.MatchID][ann.PeerID] = true
				readyMutex.Unlock()
			}
		}
	}()

	validatorTopic, err = pubSub.Join(ValidatorTopicName)
	if err == nil {
		valSub, _ := validatorTopic.Subscribe()
		go func() {
			for {
				msg, err := valSub.Next(ctx)
				if err != nil {
					return
				}
				if msg.ReceivedFrom == h.ID() {
					continue
				}

				var wrapper ValidatorMessage
				if err := json.Unmarshal(msg.Data, &wrapper); err != nil {
					continue
				}

				if wrapper.Type == "PROPOSAL" {
					var proposal BlockProposal
					json.Unmarshal(wrapper.Payload, &proposal)
					go handleBlockProposal(proposal)
				} else if wrapper.Type == "SIGNATURE" {
					var sig BlockSignature
					json.Unmarshal(wrapper.Payload, &sig)
					handleBlockSignature(sig)
				}
			}
		}()
	}

	matchFeedTopic, _ = pubSub.Join("eden-matches")
	matchSub, _ := matchFeedTopic.Subscribe()
	go func() {
		for {
			msg, err := matchSub.Next(ctx)
			if err != nil {
				return
			}
			if msg.ReceivedFrom == h.ID() {
				continue
			}

			var ann MatchAnnouncement
			if err := json.Unmarshal(msg.Data, &ann); err == nil {
				matchesMutex.Lock()
				if time.Now().Unix()-ann.Timestamp < 60 {
					networkMatches[ann.MatchID] = ann
				}
				matchesMutex.Unlock()
			}
		}
	}()

	queueTopic, err = pubSub.Join(QueueTopicName)
	if err == nil {
		queueSub, _ := queueTopic.Subscribe()
		go func() {
			for {
				msg, err := queueSub.Next(ctx)
				if err != nil {
					return
				}

				var ticket MatchmakingTicket
				if err := json.Unmarshal(msg.Data, &ticket); err == nil {
					queueMutex.Lock()
					activeTickets[ticket.TicketID] = ticket
					queueMutex.Unlock()
					if inQueue {
						go TryFormLobby(ticket.Mode, ticket.ExpectedPlayers)
					}
				}
			}
		}()
	}

	proposalTopic, err = pubSub.Join(ProposalTopicName)
	if err == nil {
		proposalSub, _ := proposalTopic.Subscribe()
		go func() {
			for {
				msg, err := proposalSub.Next(ctx)
				if err != nil {
					return
				}
				if msg.ReceivedFrom == h.ID() {
					continue
				}

				var proposal LobbyProposal
				if err := json.Unmarshal(msg.Data, &proposal); err == nil {
					go HandleLobbyProposal(proposal)
				}
			}
		}()
	}
}

func broadcastBlock(b Block) {
	if blockTopic == nil {
		return
	}
	data, _ := json.Marshal(b)
	blockTopic.Publish(ctx, data)
}

func TryFormLobby(mode string, requiredPlayers int) {
	queueMutex.RLock()
	if !inQueue {
		queueMutex.RUnlock()
		return
	}

	now := time.Now().Unix()
	var validTickets []MatchmakingTicket

	for id, t := range activeTickets {
		if now-t.Timestamp > 60 {
			delete(activeTickets, id)
			continue
		}
		if t.Mode == mode && t.ExpectedPlayers == requiredPlayers {
			validTickets = append(validTickets, t)
		}
	}
	queueMutex.RUnlock()

	if len(validTickets) == 0 {
		return
	}

	sort.Slice(validTickets, func(i, j int) bool {
		return validTickets[i].AverageElo < validTickets[j].AverageElo
	})

	var selectedTickets []MatchmakingTicket
	var currentPlayers int

	for i := 0; i < len(validTickets); i++ {
		selectedTickets = []MatchmakingTicket{}
		currentPlayers = 0

		timeInQueue := now - validTickets[i].Timestamp
		maxEloSpread := 150.0 + (float64(timeInQueue) * 10.0)

		if requiredPlayers >= 64 {
			maxEloSpread += 300.0
		}

		baseElo := validTickets[i].AverageElo

		for j := i; j < len(validTickets); j++ {
			EdenChain.Mutex.RLock()
			isBanned := false
			for _, p := range validTickets[j].PartyMembers {
				if banExpiry, exists := EdenChain.QueueBans[p]; exists && banExpiry > now {
					isBanned = true
					break
				}
			}
			EdenChain.Mutex.RUnlock()

			if isBanned {
				continue
			}

			if currentPlayers+len(validTickets[j].PartyMembers) <= requiredPlayers {
				if (validTickets[j].AverageElo - baseElo) > maxEloSpread {
					break
				}

				selectedTickets = append(selectedTickets, validTickets[j])
				currentPlayers += len(validTickets[j].PartyMembers)

				if currentPlayers == requiredPlayers {
					go ProposeLobby(selectedTickets, mode)
					return
				}
			}
		}
	}
}

func ElectHost(players []string) string {
	bestHost := players[0]
	bestScore := -99999.0

	EdenChain.Mutex.RLock()
	defer EdenChain.Mutex.RUnlock()

	for _, p := range players {
		prof := EdenChain.GetOrInitProfile(p)
		reliabilityScore := float64(prof.Matches)*10.0 + prof.Rating

		latencyPenalty := 1000.0
		pid, err := peer.Decode(p)
		if err == nil {
			latency := h.Peerstore().LatencyEWMA(pid)
			if latency > 0 {
				latencyPenalty = float64(latency.Milliseconds())
			} else if p == h.ID().String() {
				latencyPenalty = 0.0
			}
		}

		finalScore := reliabilityScore - (latencyPenalty * 5.0)
		if finalScore > bestScore {
			bestScore = finalScore
			bestHost = p
		}
	}
	return bestHost
}

func ProposeLobby(tickets []MatchmakingTicket, mode string) {
	var allPlayers []string
	var totalElo float64

	for _, t := range tickets {
		allPlayers = append(allPlayers, t.PartyMembers...)
		totalElo += (t.AverageElo * float64(len(t.PartyMembers)))
	}

	sort.Strings(allPlayers)
	hostID := ElectHost(allPlayers)
	proposalHash := sha256.Sum256([]byte(strings.Join(allPlayers, "")))

	proposal := LobbyProposal{
		ProposalID: fmt.Sprintf("prop_%x", proposalHash),
		HostID:     hostID,
		Mode:       mode,
		Players:    allPlayers,
		AverageElo: totalElo / float64(len(allPlayers)),
		Timestamp:  time.Now().Unix(),
	}

	data, _ := json.Marshal(proposal)
	if proposalTopic != nil {
		proposalTopic.Publish(ctx, data)
	}

	HandleLobbyProposal(proposal)
}

func HandleLobbyProposal(proposal LobbyProposal) {
	queueMutex.RLock()
	if !inQueue {
		queueMutex.RUnlock()
		return
	}
	queueMutex.RUnlock()

	amIIncluded := false
	for _, p := range proposal.Players {
		if p == h.ID().String() {
			amIIncluded = true
			break
		}
	}
	if !amIIncluded {
		return
	}

	proposalMutex.Lock()
	defer proposalMutex.Unlock()
	if bestProposal.ProposalID == "" || proposal.AverageElo > bestProposal.AverageElo || (proposal.AverageElo == bestProposal.AverageElo && proposal.ProposalID < bestProposal.ProposalID) {
		bestProposal = proposal

		if proposalTimer != nil {
			proposalTimer.Stop()
		}

		proposalTimer = time.AfterFunc(5*time.Second, func() {
			LockInLobby()
		})
	}
}

func handleBlockProposal(proposal BlockProposal) {
	EdenChain.Mutex.RLock()
	myProfile := EdenChain.GetOrInitProfile(myPeerID)
	lastHast := EdenChain.LastBlock.Hash
	EdenChain.Mutex.RUnlock()

	if myProfile.StakedEDN <= 0 {
		return
	}

	if proposal.BlockData.PrevHash != lastHast {
		return
	}

	hashBytes, _ := hex.DecodeString(proposal.BlockData.Hash)
	privBytes, _ := hex.DecodeString(myPrivKey)
	privKey, _ := x509.ParseECPrivateKey(privBytes)
	r, s, _ := ecdsa.Sign(rand.Reader, privKey, hashBytes)

	sigBytes := make([]byte, 64)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(sigBytes[32-len(rBytes):32], rBytes)
	copy(sigBytes[64-len(sBytes):64], sBytes)
	sigHex := hex.EncodeToString(sigBytes)

	sigMsg := BlockSignature{
		ValidatorID: myPeerID,
		BlockHash:   proposal.BlockData.Hash,
		Signature:   sigHex,
	}

	payload, _ := json.Marshal(sigMsg)
	wrapper := ValidatorMessage{Type: "SIGNATURE", Payload: payload}
	data, _ := json.Marshal(wrapper)

	if validatorTopic != nil {
		validatorTopic.Publish(ctx, data)
	}
}

func handleBlockSignature(sig BlockSignature) {
	sigsMutex.Lock()
	defer sigsMutex.Unlock()

	if pendingBlockSigs[sig.BlockHash] == nil {
		pendingBlockSigs[sig.BlockHash] = make(map[string]string)
	}
	pendingBlockSigs[sig.BlockHash][sig.ValidatorID] = sig.Signature
}

//export RegisterMatchCallback
func RegisterMatchCallback(fn C.MatchFoundCallbackFn) {
	C.SetMatchFoundCallback(fn)
}

//export LeaveMatchmaking
func LeaveMatchmaking() {
	queueMutex.Lock()
	defer queueMutex.Unlock()

	if !inQueue {
		return
	}
	inQueue = false
	if myCurrentTicket != "" {
		delete(activeTickets, myCurrentTicket)
		myCurrentTicket = ""
	}
}

func LockInLobby() {
	proposalMutex.Lock()
	finalProposal := bestProposal
	bestProposal = LobbyProposal{}
	proposalMutex.Unlock()

	if finalProposal.ProposalID == "" {
		return
	}

	LeaveMatchmaking()

	cMatchID := C.CString(finalProposal.ProposalID)
	cHostID := C.CString(finalProposal.HostID)
	rosterStr := strings.Join(finalProposal.Players, ",")
	cRoster := C.CString(rosterStr)
	C.CallMatchFound(cMatchID, cHostID, cRoster)
	C.free(unsafe.Pointer(cMatchID))
	C.free(unsafe.Pointer(cHostID))
	C.free(unsafe.Pointer(cRoster))
}

//export BroadcastMatchReady
func BroadcastMatchReady(matchID *C.char) {
	mID := C.GoString(matchID)

	ann := MatchReadyBroadcast{
		MatchID: mID,
		PeerID:  h.ID().String(),
	}

	if data, err := json.Marshal(ann); err == nil {
		readyFeedTopic.Publish(ctx, data)
		readyMutex.Lock()
		if matchReadyStates[mID] == nil {
			matchReadyStates[mID] = make(map[string]bool)
		}
		matchReadyStates[mID][h.ID().String()] = true
		readyMutex.Unlock()
	}
}

//export GetMatchReadyStates
func GetMatchReadyStates(matchID *C.char) *C.char {
	mID := C.GoString(matchID)

	readyMutex.RLock()
	defer readyMutex.RUnlock()

	states, exists := matchReadyStates[mID]
	if !exists {
		return C.CString("{}")
	}

	data, _ := json.Marshal(states)
	return C.CString(string(data))
}

//export GetMatchRoster
func GetMatchRoster(matchID *C.char) *C.char {
	mID := C.GoString(matchID)

	EdenChain.Mutex.RLock()
	defer EdenChain.Mutex.RUnlock()

	session, exists := EdenChain.MatchSessions[mID]
	if !exists {
		return C.CString("[]")
	}

	data, err := json.Marshal(session.Roster)
	if err != nil {
		return C.CString("[]")
	}
	return C.CString(string(data))
}

//export GetMyPeerID
func GetMyPeerID() *C.char {
	return C.CString(myPeerID)
}

//export RespondToFriendRequest
func RespondToFriendRequest(peerID *C.char, accept C.int) *C.char {
	pIDStr := C.GoString(peerID)
	isAccepting := int(accept) == 1
	friendMutex.Lock()
	entry, exists := MyFriends[pIDStr]
	friendMutex.Unlock()

	if !exists {
		return C.CString("Error: Friend request not found")
	}

	if isAccepting {
		friendMutex.Lock()
		entry.Status = "Confirmed"
		MyFriends[pIDStr] = entry
		friendMutex.Unlock()
		SaveFriends()
		go SendFriendSignal(pIDStr, "ACCEPT")
		return C.CString("Success: Friend Accepted")
	} else {
		friendMutex.Lock()
		delete(MyFriends, pIDStr)
		friendMutex.Unlock()
		SaveFriends()
		go SendFriendSignal(pIDStr, "REJECT")
		return C.CString("Success: Request Rejected")
	}
}

//export AdvertiseHostLobby
func AdvertiseHostLobby(mode *C.char, mapName *C.char) {
	modeStr := C.GoString(mode)
	mapStr := C.GoString(mapName)
	advString := fmt.Sprintf("%s-%s-%s-host", GlobalAppID, modeStr, mapStr)

	rd := routing.NewRoutingDiscovery(kademliaDHT)
	dutil.Advertise(ctx, rd, advString)
}

//export SubmitDodgePenalty
func SubmitDodgePenalty(matchID *C.char, dodgerPeerID *C.char) *C.char {
	mID := C.GoString(matchID)
	dodgerID := C.GoString(dodgerPeerID)

	pubKeyBytes, err := hex.DecodeString(myPubKey)
	if err != nil {
		return C.CString("Error: Invalid Public Key")
	}

	payload := fmt.Sprintf("%s:%s", mID, dodgerID)

	tx := Transaction{
		ID:        fmt.Sprintf("pen_%d", time.Now().UnixNano()),
		Type:      TxTypePenalty,
		Sender:    h.ID().String(),
		Receiver:  "CONSENSUS_ENGINE",
		Amount:    0,
		Payload:   payload,
		Timestamp: time.Now().Unix(),
		PublicKey: pubKeyBytes,
		Nonce:     GetNextNonce(h.ID().String()),
	}

	if err := SignTransaction(myPrivKey, &tx); err != nil {
		return C.CString("Error: Signing Failed")
	}

	EdenChain.Mutex.RLock()
	lastIndex := EdenChain.LastBlock.Index
	prevHash := EdenChain.LastBlock.Hash
	EdenChain.Mutex.RUnlock()

	newBlock := Block{
		Index:        lastIndex + 1,
		Timestamp:    time.Now().Unix(),
		Transactions: []Transaction{tx},
		PrevHash:     prevHash,
	}
	newBlock.Hash = calculateHash(newBlock)

	if EdenChain.AddBlock(newBlock) {
		broadcastBlock(newBlock)
		return C.CString("Success: Penalty Broadcasted")
	}
	return C.CString("Error: Block Rejected")
}

//export GetValidatorMetrics
func GetValidatorMetrics(peerID *C.char) *C.char {
	pid := C.GoString(peerID)
	if pid == "" {
		pid = h.ID().String()
	}

	EdenChain.Mutex.RLock()
	profile := EdenChain.GetOrInitProfile(pid)
	EdenChain.Mutex.RUnlock()

	accuracy := 0.0
	if profile.TribunalTotalVotes > 0 {
		accuracy = (float64(profile.TribunalCorrect) / float64(profile.TribunalTotalVotes)) * 100.0
	}

	metrics := map[string]interface{}{
		"demos_parsed": profile.TribunalDemosParsed,
		"edn_earned":   profile.TribunalEDNEarned,
		"accuracy":     accuracy,
		"staked_edn":   profile.StakedEDN,
	}

	data, _ := json.Marshal(metrics)
	return C.CString(string(data))
}

//export AutoConnectToPeers
func AutoConnectToPeers(targetMode *C.char, targetMap *C.char) *C.char {
	if kademliaDHT == nil {
		return C.CString("DHT Not Ready")
	}

	modeStr := C.GoString(targetMode)
	mapStr := C.GoString(targetMap)
	searchString := fmt.Sprintf("%s-%s-%s-host", GlobalAppID, modeStr, mapStr)

	rd := routing.NewRoutingDiscovery(kademliaDHT)
	searchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	peerChan, _ := rd.FindPeers(searchCtx, searchString)

	for p := range peerChan {
		if p.ID == h.ID() || len(p.Addrs) == 0 {
			continue
		}

		if h.Connect(ctx, p) == nil {
			s, err := h.NewStream(ctx, p.ID, ProtocolID)
			if err == nil {
				if oldWorker, exists := activeStreams[s.Conn().RemotePeer()]; exists {
					oldWorker.Close()
				}
				sw := FastStreamWorker(ctx, s)
				streamLock.Lock()
				activeStreams[s.Conn().RemotePeer()] = sw
				routingTable[getIPFromPeerID(s.Conn().RemotePeer().String())] = sw
				streamLock.Unlock()
				go readStreamLoop(s)
				return C.CString(p.ID.String())
			}
			go TriggerSync(p.ID)
		}
	}
	return C.CString("No peers found")
}

//export ConnectToPeer
func ConnectToPeer(peerID *C.char) {
	if h == nil {
		return
	}
	pIDStr := C.GoString(peerID)
	targetID, err := peer.Decode(pIDStr)
	if err != nil {
		return
	}

	peerInfo, err := kademliaDHT.FindPeer(ctx, targetID)
	if err != nil {
		return
	}

	if err := h.Connect(ctx, peerInfo); err == nil {
		s, err := h.NewStream(ctx, targetID, ProtocolID)
		if err == nil {
			if oldWorker, exists := activeStreams[s.Conn().RemotePeer()]; exists {
				oldWorker.Close()
			}
			sw := FastStreamWorker(ctx, s)
			streamLock.Lock()
			activeStreams[s.Conn().RemotePeer()] = sw
			routingTable[getIPFromPeerID(s.Conn().RemotePeer().String())] = sw
			streamLock.Unlock()
			go readStreamLoop(s)
		}
		go TriggerSync(targetID)
	}
}

//export EnterMatchmaking
func EnterMatchmaking(mode *C.char, partyList *C.char, expectedPlayers C.int) *C.char {
	if pubSub == nil || queueTopic == nil {
		return C.CString("Error: PubSub Not Ready")
	}

	modeStr := C.GoString(mode)
	partyStr := C.GoString(partyList)
	party := strings.Split(partyStr, ",")
	targetCount := int(expectedPlayers)

	if targetCount < 2 {
		targetCount = 2
	}
	if targetCount > 128 {
		targetCount = 128
	}

	EdenChain.Mutex.RLock()
	var totalElo float64 = 0
	for _, memberID := range party {
		profile := EdenChain.GetOrInitProfile(memberID)
		totalElo += profile.Rating
	}
	avgElo := totalElo / float64(len(party))
	EdenChain.Mutex.RUnlock()

	ticket := MatchmakingTicket{
		TicketID:        fmt.Sprintf("tk_%d_%s", time.Now().UnixNano(), h.ID().String()[:6]),
		LeaderID:        h.ID().String(),
		PartyMembers:    party,
		AverageElo:      avgElo,
		Mode:            modeStr,
		ExpectedPlayers: targetCount,
		Timestamp:       time.Now().Unix(),
	}

	data, err := json.Marshal(ticket)
	if err != nil {
		return C.CString("Error: Failed to serialize ticket")
	}

	queueTopic.Publish(ctx, data)
	queueMutex.Lock()
	inQueue = true
	myCurrentTicket = ticket.TicketID
	activeTickets[ticket.TicketID] = ticket
	queueMutex.Unlock()

	go TryFormLobby(modeStr, targetCount)

	go func(t MatchmakingTicket, payload []byte) {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				queueMutex.RLock()
				stillInQueue := inQueue && myCurrentTicket == t.TicketID
				queueMutex.RUnlock()

				if !stillInQueue {
					return
				}

				if queueTopic != nil {
					t.Timestamp = time.Now().Unix()
					freshData, _ := json.Marshal(t)
					queueTopic.Publish(ctx, freshData)
				}
			}
		}
	}(ticket, data)

	return C.CString("Success: In Queue")
}

//export BroadcastMapVeto
func BroadcastMapVeto(matchID *C.char, mapName *C.char) {
	mID := C.GoString(matchID)
	mName := C.GoString(mapName)

	v := VetoBroadcast{MatchID: mID, PeerID: h.ID().String(), MapName: mName}
	if data, err := json.Marshal(v); err == nil {
		if vetoTopic != nil {
			vetoTopic.Publish(ctx, data)
		}

		vetoMutex.Lock()
		matchVetoes[mID] = append(matchVetoes[mID], mName)
		vetoMutex.Unlock()
	}
}

//export GetMatchVetoes
func GetMatchVetoes(matchID *C.char) *C.char {
	mID := C.GoString(matchID)

	vetoMutex.RLock()
	defer vetoMutex.RUnlock()

	bans, exists := matchVetoes[mID]
	if !exists {
		return C.CString("[]")
	}

	data, _ := json.Marshal(bans)
	return C.CString(string(data))
}

//export RegisterMatchEndedCallback
func RegisterMatchEndedCallback(fn C.MatchEndedCallbackFn) {
	C.SetMatchEndedCallback(fn)
}

//export GetMyBanExpiry
func GetMyBanExpiry() *C.char {
	EdenChain.Mutex.RLock()
	defer EdenChain.Mutex.RUnlock()

	expiry := EdenChain.QueueBans[h.ID().String()]
	return C.CString(fmt.Sprintf("%d", expiry))
}

//export IsPeerAlive
func IsPeerAlive() C.int {
	if h == nil {
		return 0
	}
	if len(h.Network().Peers()) > 0 {
		return 1
	}
	return 0
}

//export GetIPForPeer
func GetIPForPeer(peerIDStr *C.char) *C.char {
	return C.CString(getIPFromPeerID(C.GoString(peerIDStr)))
}

func getIPFromPeerID(pid string) string {
	h := sha256.Sum256([]byte(pid))
	return fmt.Sprintf("10.%d.%d.%d", h[0], h[1], h[2])
}

//export FreeString
func FreeString(str *C.char) {
	C.free(unsafe.Pointer(str))
}

func main() {}
