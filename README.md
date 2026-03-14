# Eden P2P Network for Unreal Engine 5

**Eden** is a comprehensive, decentralized peer-to-peer multiplayer networking ecosystem, VPN tunnel, and blockchain consensus engine built natively for Unreal Engine 5. It seamlessly bridges advanced Libp2p networking, Wintun-backed virtual LAN routing, and a bespoke LevelDB blockchain layer to power competitive, serverless multiplayer experiences.

Whether you are building a tactical shooter requiring Faceit-like matchmaking and Glicko-2 skill tracking, or a decentralized game featuring integrated betting and community-driven anti-cheat, Eden provides the entire infrastructure stack directly via Unreal Engine Blueprints.

---

## 🌟 Core Features

### 🌐 Decentralized Networking & VPN Tunneling
* **Zero-Config P2P Networking:** Utilizes `libp2p` with automated NAT traversal and Hole Punching to directly connect players without dedicated servers.
* **Integrated Wintun Driver:** Automatically provisions a virtual network adapter (`EDNAdapter`) that wraps game traffic inside a secure, encrypted tunnel, allowing traditional LAN-based game architectures to function globally.
* **Seamless UE5 Integration:** Exposed entirely to Blueprints via the `EdenBPLibrary`. Start the engine, find matches, and advertise lobbies with single nodes.

### ⚔️ Competitive Matchmaking
* **Faceit-Style Queues:** Decentralized queueing system via PubSub topics. Players can form parties, enter matchmaking, and mathematically discover optimal lobbies based on party size and Elo spread.
* **Glicko-2 / Abel2 Rating System:** Built-in skill tracking that accounts for rating, deviation, and volatility. Rewards and rating adjustments are automatically calculated upon match consensus.
* **Pre-Match Systems:** Built-in support for Map Vetoes, Match Ready states, and secure password-protected lobby generation. 

### 💎 Blockchain Economy & Consensus
* **EdenCoin Ledger:** A LevelDB-backed blockchain engine running locally on nodes. Supports secure wallet generation (ECDSA), transfers, and balances.
* **Decentralized Betting Pools:** Integrated smart-contract-like betting. Players can stake EdenCoin on match outcomes (`Team A` vs `Team B`), with automated payouts handled by the consensus engine upon match resolution.
* **Game Proofs (PoW):** Hosts act as "miners," submitting match durations, player counts, and connection quality metrics to the network to earn system-minted block rewards.

### ⚖️ The Tribunal & Leaver Buster
* **Community Anti-Cheat:** Staked validators can review match outcomes and vote on suspected malicious actors. If a consensus threshold is met, the suspect is penalized and honest validators earn a share of slashed funds.
* **Dodge Penalties:** Integrated Leaver Buster instantly applies queue bans and rating penalties to players who abandon matches or dodge queues.

---

## 🏗 Architecture

Eden operates across three primary layers:
1. **`Eden` (UE5 C++ Plugin):** The front-facing interface for Unreal Engine, exposing Blueprint nodes and managing the lifecycle of the underlying DLLs.
2. **`UE5adam.dll` (C/C++ Bridge):** Handles OS-level operations, provisions the Wintun virtual network adapter, and routes raw packet data between the OS and the Go network layer.
3. **`UE5cain.dll` (Go Core):** The heavy lifter compiled from Go. Contains the `libp2p` node logic (`mainUE.go`) and the blockchain/consensus state machine (`eveUE.go`).

---

## 🚀 Installation & Setup

### Prerequisites
* Unreal Engine 5.x
* Windows 64-bit target environment

### Deployment Steps
1. Navigate to your UE5 project's root directory.
2. Create a `Plugins` folder if one does not exist.
3. Extract the `Eden` plugin folder into the `Plugins` directory.
4. Ensure the pre-compiled binaries are located in `Plugins/Eden/Binaries/Win64/`:
   * `UE5adam.dll`
   * `UE5cain.dll`
   * `wintun.dll`
5. Launch your Unreal Engine project and enable the **Eden P2P Network** plugin via `Edit > Plugins`.

---

## 🔌 Quick Start (Blueprint API)

All core functions are accessible via the `Eden` category in Blueprints. 

* **Initialization:** Call `Start Eden Engine (Game ID)` on game startup to spin up the P2P node and virtual adapter.
* **Matchmaking:** Use `Enter Matchmaking` to broadcast a ticket to the network. Bind to the callback to receive match credentials once a lobby is formed.
* **Wallets & Betting:** Use `Get Balance` to check a player's funds, and `Place Bet` once a match ID is established to wager EdenCoin on a specific team.
* **Shutdown:** Always call `Stop Eden Engine` on `EndPlay` or game exit to safely terminate the tunnel and release the adapter.

---

## 🛡️ Security & Privacy
* All peer-to-peer traffic is secured and authenticated using ECDSA keys derived upon first launch.
* Passwords and sensitive match data are encrypted using AES-GCM via shared secrets (ECDH).

---

## 📄 License
This project is licensed under the MIT License - see the `license.text` file for details.