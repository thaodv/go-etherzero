// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package eth

import (
	"context"
	"math/big"

	"github.com/etherzero/go-etherzero/accounts"
	"github.com/etherzero/go-etherzero/common"
	"github.com/etherzero/go-etherzero/common/math"
	"github.com/etherzero/go-etherzero/core"
	"github.com/etherzero/go-etherzero/core/bloombits"
	"github.com/etherzero/go-etherzero/core/state"
	"github.com/etherzero/go-etherzero/core/types"
	"github.com/etherzero/go-etherzero/core/vm"
	"github.com/etherzero/go-etherzero/eth/downloader"
	"github.com/etherzero/go-etherzero/eth/gasprice"
	"github.com/etherzero/go-etherzero/ethdb"
	"github.com/etherzero/go-etherzero/event"
	"github.com/etherzero/go-etherzero/params"
	"github.com/etherzero/go-etherzero/rpc"
	"encoding/hex"
	"strings"
	"fmt"
	"github.com/etherzero/go-etherzero/p2p/discover"
	"github.com/etherzero/go-etherzero/enodetools"
)

// EthAPIBackend implements ethapi.Backend for full nodes
type EthAPIBackend struct {
	eth *Ethereum
	gpo *gasprice.Oracle
}

// ChainConfig returns the active chain configuration.
func (b *EthAPIBackend) ChainConfig() *params.ChainConfig {
	return b.eth.chainConfig
}

func (b *EthAPIBackend) CurrentBlock() *types.Block {
	return b.eth.blockchain.CurrentBlock()
}

func (b *EthAPIBackend) SetHead(number uint64) {
	b.eth.protocolManager.downloader.Cancel()
	b.eth.blockchain.SetHead(number)
}

func (b *EthAPIBackend) HeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Header, error) {
	// Pending block is only known by the miner
	if blockNr == rpc.PendingBlockNumber {
		block := b.eth.miner.PendingBlock()
		return block.Header(), nil
	}
	// Otherwise resolve and return the block
	if blockNr == rpc.LatestBlockNumber {
		return b.eth.blockchain.CurrentBlock().Header(), nil
	}
	return b.eth.blockchain.GetHeaderByNumber(uint64(blockNr)), nil
}

func (b *EthAPIBackend) HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error) {
	return b.eth.blockchain.GetHeaderByHash(hash), nil
}

func (b *EthAPIBackend) BlockByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Block, error) {
	// Pending block is only known by the miner
	if blockNr == rpc.PendingBlockNumber {
		block := b.eth.miner.PendingBlock()
		return block, nil
	}
	// Otherwise resolve and return the block
	if blockNr == rpc.LatestBlockNumber {
		return b.eth.blockchain.CurrentBlock(), nil
	}
	return b.eth.blockchain.GetBlockByNumber(uint64(blockNr)), nil
}

func (b *EthAPIBackend) StateAndHeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*state.StateDB, *types.Header, error) {
	// Pending state is only known by the miner
	if blockNr == rpc.PendingBlockNumber {
		block, state := b.eth.miner.Pending()
		return state, block.Header(), nil
	}
	// Otherwise resolve the block number and return its state
	header, err := b.HeaderByNumber(ctx, blockNr)
	if header == nil || err != nil {
		return nil, nil, err
	}
	stateDb, err := b.eth.BlockChain().StateAt(header.Root)
	return stateDb, header, err
}

func (b *EthAPIBackend) GetBlock(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return b.eth.blockchain.GetBlockByHash(hash), nil
}

func (b *EthAPIBackend) GetReceipts(ctx context.Context, hash common.Hash) (types.Receipts, error) {
	return b.eth.blockchain.GetReceiptsByHash(hash), nil
}

func (b *EthAPIBackend) GetLogs(ctx context.Context, hash common.Hash) ([][]*types.Log, error) {
	receipts := b.eth.blockchain.GetReceiptsByHash(hash)
	if receipts == nil {
		return nil, nil
	}
	logs := make([][]*types.Log, len(receipts))
	for i, receipt := range receipts {
		logs[i] = receipt.Logs
	}
	return logs, nil
}

func (b *EthAPIBackend) GetTd(blockHash common.Hash) *big.Int {
	return b.eth.blockchain.GetTdByHash(blockHash)
}

func (b *EthAPIBackend) GetEVM(ctx context.Context, msg core.Message, state *state.StateDB, header *types.Header, vmCfg vm.Config) (*vm.EVM, func() error, error) {
	state.SetBalance(msg.From(), math.MaxBig256, header.Number)
	state.SetPower(msg.From(), math.MaxBig256)
	vmError := func() error { return nil }

	context := core.NewEVMContext(msg, header, b.eth.BlockChain(), nil)
	return vm.NewEVM(context, state, b.eth.chainConfig, vmCfg), vmError, nil
}

func (b *EthAPIBackend) SubscribeRemovedLogsEvent(ch chan<- core.RemovedLogsEvent) event.Subscription {
	return b.eth.BlockChain().SubscribeRemovedLogsEvent(ch)
}

func (b *EthAPIBackend) SubscribeChainEvent(ch chan<- core.ChainEvent) event.Subscription {
	return b.eth.BlockChain().SubscribeChainEvent(ch)
}

func (b *EthAPIBackend) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return b.eth.BlockChain().SubscribeChainHeadEvent(ch)
}

func (b *EthAPIBackend) SubscribeChainSideEvent(ch chan<- core.ChainSideEvent) event.Subscription {
	return b.eth.BlockChain().SubscribeChainSideEvent(ch)
}

func (b *EthAPIBackend) SubscribeLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return b.eth.BlockChain().SubscribeLogsEvent(ch)
}

func (b *EthAPIBackend) SendTx(ctx context.Context, signedTx *types.Transaction) error {
	return b.eth.txPool.AddLocal(signedTx)
}

func (b *EthAPIBackend) GetPoolTransactions() (types.Transactions, error) {
	pending, err := b.eth.txPool.Pending()
	if err != nil {
		return nil, err
	}
	var txs types.Transactions
	for _, batch := range pending {
		txs = append(txs, batch...)
	}
	return txs, nil
}

func (b *EthAPIBackend) GetPoolTransaction(hash common.Hash) *types.Transaction {
	return b.eth.txPool.Get(hash)
}

func (b *EthAPIBackend) GetPoolNonce(ctx context.Context, addr common.Address) (uint64, error) {
	return b.eth.txPool.State().GetNonce(addr), nil
}

func (b *EthAPIBackend) Stats() (pending int, queued int) {
	return b.eth.txPool.Stats()
}

func (b *EthAPIBackend) TxPoolContent() (map[common.Address]types.Transactions, map[common.Address]types.Transactions) {
	return b.eth.TxPool().Content()
}

func (b *EthAPIBackend) SubscribeNewTxsEvent(ch chan<- core.NewTxsEvent) event.Subscription {
	return b.eth.TxPool().SubscribeNewTxsEvent(ch)
}

func (b *EthAPIBackend) Downloader() *downloader.Downloader {
	return b.eth.Downloader()
}

func (b *EthAPIBackend) ProtocolVersion() int {
	return b.eth.EthVersion()
}

func (b *EthAPIBackend) SuggestPrice(ctx context.Context) (*big.Int, error) {
	return b.gpo.SuggestPrice(ctx)
}

func (b *EthAPIBackend) ChainDb() ethdb.Database {
	return b.eth.ChainDb()
}

func (b *EthAPIBackend) EventMux() *event.TypeMux {
	return b.eth.EventMux()
}

func (b *EthAPIBackend) AccountManager() *accounts.Manager {
	return b.eth.AccountManager()
}

func (b *EthAPIBackend) BloomStatus() (uint64, uint64) {
	sections, _, _ := b.eth.bloomIndexer.Sections()
	return params.BloomBitsBlocks, sections
}

func (b *EthAPIBackend) ServiceFilter(ctx context.Context, session *bloombits.MatcherSession) {
	for i := 0; i < bloomFilterThreads; i++ {
		go session.Multiplex(bloomRetrievalBatch, bloomRetrievalWait, b.eth.bloomRequests)
	}
}

// Masternodes return masternode info
func (b *EthAPIBackend) Masternodes() []string {
	list, _ := b.eth.masternodeManager.MasternodeList(b.eth.blockchain.CurrentBlock().Number())
	return list
}

// GetInfo return related info in masternode contract
func (b *EthAPIBackend) GetInfo(nodeid string) string {
	var id [8]byte
	node, err := hex.DecodeString(strings.TrimPrefix(nodeid, "0x"))
	if err != nil {
		fmt.Printf("err %v\n", err)
		return ""
	} else if len(node) != len(id) {
		return ""
	}
	copy(id[:], node)
	fmt.Println("nodeid ", nodeid)

	info, err := b.eth.masternodeManager.contract.GetInfo(nil, id)
	if err != nil {
		fmt.Errorf("contract.Has", "error", err)
		return ""
	}

	return fmt.Sprintf("Id1: %v,Id2:%v,PreId:0x%v,NextId:0x%v,BlockNumber:%v,Account:%v,BlockOnlineAcc:%v,BloakLastPing:%v",
		common.BytesToHash(info.Id1[:]).String(), common.BytesToHash(info.Id2[:]).String(), common.Bytes2Hex(info.PreId[:]), common.Bytes2Hex(info.NextId[:]), info.BlockNumber.String(), info.Account.String(),
		info.BlockOnlineAcc.String(), info.BlockLastPing.String())
}

// GetEnode named by id
func (b *EthAPIBackend) GetEnode(nodeid string) (enodeinfo string) {
	if b.eth.masternodeManager.enodeinfoContract == nil {
		enodeinfo = "wait for 10 seconds until finish initializing"
		return
	}

	var id [8]byte
	nodebyte, err := hex.DecodeString(strings.TrimPrefix(nodeid, "0x"))
	if err != nil {
		fmt.Printf("err %v\n", err)
		enodeinfo = fmt.Sprintf("nodeid is illegal  %v", nodeid)
		return
	}
	fmt.Printf("nodebyte is %v", len(nodebyte))

	if nodebyte[:] == nil || len(nodebyte) != int(8) {
		enodeinfo = fmt.Sprintf("nodeid is illegal  %v\n", nodebyte)
		return
	}

	copy(id[:], nodebyte)
	fmt.Printf("nodeid %v \n", id)

	data, err := b.eth.masternodeManager.enodeinfoContract.GetSingleEnode(nil, id)
	if err != nil {
		fmt.Errorf("enodeinfoContract.GetSingleEnode error %v\n", err)
		return
	}
	fmt.Printf("data.Id1 %v ,data.Id2 %v,data.IpPort %v\n", data.Id1, data.Id2, data.Ipport)

	if data.Id1 == [32]byte{} ||
		data.Id2 == [32]byte{} ||
		len(data.Id1) != 32 ||
		len(data.Id2) != 32 ||
		data.Ipport == uint64(0) {
		enodeinfo = fmt.Sprintf("No enodeinfo storaged for nodeid %v", nodeid)
		return
	}
	// masternode.getEnode("0x7ec780bcd5488bcf")
	// personal.unlockAccount("0xf9037710c273d0321ddd1b6042d211c3703829db","123",0)
	// miner.start()
	// txpool.content
	// masternode.list
	// miner.stop()
	// eth.mining
	// eth.blockNumber
	//str := common.Bytes2Hex(data.Ipport[:])
	//ip_int, err := strconv.Atoi(str)
	//if err != nil {
	//	fmt.Printf("strconv.Atoistrconv.Atoistrconv.Atoistrconv.Atoi %v", err)
	//	return
	// 0xf170eef0984d24eb479c508a4c650d2c383f06438f050a16fced1d07982c720c 0
	// 0x3892b10387ab33372e3e3acf074d32c87d6b24c056155fa5796d83902da0522d 1
	// 0x3cc872d7b889032b3daa4f112ac43cbc482f4c2dda20d3d4ff94ee8bf23b1cf6 2
	//}


	node := enodetools.NewDiscoverNode(data.Id1, data.Id2, data.Ipport)
	return node.String()
}

// Masternodes return masternode contract data
func (b *EthAPIBackend) Data() (strPromotion string) {
	if b.eth.masternodeManager.srvr.Self() == nil {
		strPromotion = "wait for more 10 seconds to initial the geth"
		return
	}
	xy := b.eth.masternodeManager.srvr.Self().XY()

	var id [8]byte
	copy(id[:], xy[0:8])
	has, err := b.eth.masternodeManager.contract.Has(nil, id)
	if err != nil {
		fmt.Errorf("contract.Has error %v", err)
		return
	}
	if has {
		strPromotion = fmt.Sprintf("### It's already been a masternode!,don't send your masternode data any more!")
	}
	data := "0x2f926732" + common.Bytes2Hex(xy[:])
	return fmt.Sprintf("%v your masternode data is %v", strPromotion, data)
}

// Masternodes return masternode contract data
func (b *EthAPIBackend) Ns() int64 {
	return discover.NanoDrift()
}

// StartMasternode just call the start function of instantx
// TODO ,send 20 ether to the contract address
func (b *EthAPIBackend) StartMasternode() bool {
	//b.eth.masternodeManager.is.Start()
	return true
}

// Stop
func (b *EthAPIBackend) StopMasternode() bool {
	//b.eth.masternodeManager.is.Stop()
	return true
}
