package blockstore

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/DSiSc/blockstore/common"
	"github.com/DSiSc/blockstore/config"
	"github.com/DSiSc/blockstore/leveldbstore"
	"github.com/DSiSc/blockstore/memorystore"
	"github.com/DSiSc/blockstore/util"
	"github.com/DSiSc/craft/log"
	"github.com/DSiSc/craft/types"
	"sync"
	"sync/atomic"
)

const (
	// DB plugin
	PLUGIN_LEVELDB = "leveldb"
	// memory plugin
	PLUGIN_MEMDB = "memorydb"
	// block height before genesis block
	INIT_BLOCK_HEIGHT = 0
	// latestBlockKey tracks the latest know full block's hash.
	latestBlockKey = "LatestBlock"
)

// DBStore represent the low level database to store block
type DBStore interface {
	Put(key []byte, value []byte) error
	Get(key []byte) ([]byte, error)
	Delete(key []byte) error
}

// Block store save the data of block & transaction
type BlockStore struct {
	store        DBStore      // Block store handler
	currentBlock atomic.Value //Current block
	lock         sync.RWMutex
}

// NewBlockStore return the block store instance
func NewBlockStore(config *config.BlockStoreConfig) (*BlockStore, error) {
	log.Info("Start creating block store, with config: %v ", config)
	store, err := createDBStore(config)
	if err != nil {
		return nil, err
	}
	blockStore := &BlockStore{
		store: store,
	}

	//load latest block from database.
	blockStore.loadLatestBlock()
	return blockStore, nil
}

// init db store.
func createDBStore(config *config.BlockStoreConfig) (DBStore, error) {
	switch config.PluginName {
	case PLUGIN_LEVELDB:
		log.Debug("Create file-based block store, with file path: %s ", config.DataPath)
		return leveldbstore.NewLevelDBStore(config.DataPath)
	case PLUGIN_MEMDB:
		log.Debug("Create memory-based block store")
		return memorystore.NewMemDBStore(), nil
	default:
		log.Error("Not support plugin.")
		return nil, fmt.Errorf("Not support plugin type %s", config.PluginName)
	}
}

// load latest block from database.
func (blockStore *BlockStore) loadLatestBlock() {
	log.Info("Start loading block from database")
	blockHashByte, err := blockStore.store.Get([]byte(latestBlockKey))
	if err != nil {
		log.Warn("Failed to load latest block hash from database, we will set current block to nil")
		return
	}

	// load latest block by hash
	blockHash := util.BytesToHash(blockHashByte)
	latestBlock, err := blockStore.GetBlockByHash(blockHash)
	if err != nil {
		log.Warn("Failed to load the latest block with the hash of the record in the database, we will set current block to nil")
		return
	}
	blockStore.currentBlock.Store(latestBlock)
}

// WriteBlock write the block to database. return error if write failed.
func (blockStore *BlockStore) WriteBlock(block *types.Block) error {
	log.Info("Start writing block %v to database.", block)
	blockByte, err := encodeBlock(block)
	if err != nil {
		log.Error("Failed to encode block %v to byte, as: %v ", block, err)
		return fmt.Errorf("Failed to encode block %v to byte, as: %v ", block, err)
	}
	// write block
	blockHash := common.BlockHash(block)
	err = blockStore.store.Put(util.HashToBytes(blockHash), blockByte)
	if err != nil {
		log.Error("Failed to write block %s to database, as: %v ", blockHash, err)
		return fmt.Errorf("Failed to write block %s to database, as: %v ", blockHash, err)
	}
	// write block height and hash mapping
	err = blockStore.store.Put(encodeBlockHeight(block.Header.Height), util.HashToBytes(blockHash))
	if err != nil {
		log.Error("Failed to record the mapping between block and height")
		return fmt.Errorf("Failed to record the mapping between block and height ")
	}
	// update current block
	blockStore.recordCurrentBlock(block)
	// update latest block
	err = blockStore.store.Put([]byte(latestBlockKey), util.HashToBytes(blockHash))
	if err != nil {
		log.Warn("Failed to record latest block, as: %v. we will still use the previous latest block as current latest block ", err)
	}
	return nil
}

// GetBlockByHash get block by block hash.
func (blockStore *BlockStore) GetBlockByHash(hash types.Hash) (*types.Block, error) {
	blockByte, err := blockStore.store.Get(util.HashToBytes(hash))
	if blockByte == nil || err != nil {
		return nil, fmt.Errorf("failed to get block with hash %s, as: %s", hash, err)
	}
	block, err := decodeBlock(blockByte)
	if err != nil {
		return nil, fmt.Errorf("failed to decode block with hash %s from database as: %s", hash, err)
	}
	return block, nil
}

// GetBlockByHeight get block by height.
func (blockStore *BlockStore) GetBlockByHeight(height uint64) (*types.Block, error) {
	blockHashByte, err := blockStore.store.Get(encodeBlockHeight(height))
	if err != nil {
		return nil, fmt.Errorf("failed to get block with height %d, as: %s", height, err)
	}
	blockHash := util.BytesToHash(blockHashByte)
	return blockStore.GetBlockByHash(blockHash)
}

// GetCurrentBlock get current block.
func (blockStore *BlockStore) GetCurrentBlock() *types.Block {
	currentBlock := blockStore.currentBlock.Load()
	if currentBlock != nil {
		return currentBlock.(*types.Block)
	} else {
		return nil
	}
}

// GetCurrentBlockHeight get current block height.
func (blockStore *BlockStore) GetCurrentBlockHeight() uint64 {
	currentBlock := blockStore.GetCurrentBlock()
	if currentBlock != nil {
		return currentBlock.Header.Height
	} else {
		return INIT_BLOCK_HEIGHT
	}
}

// record current block
func (blockStore *BlockStore) recordCurrentBlock(block *types.Block) {
	log.Info("Update current block to %v", block)
	blockStore.currentBlock.Store(block)
}

// encodeBlockHeight encodes a block height to byte
func encodeBlockHeight(height uint64) []byte {
	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, height)
	return enc
}

// encode block to byte
func encodeBlock(block *types.Block) ([]byte, error) {
	return json.Marshal(block)
}

// decode block from byte
func decodeBlock(blockByte []byte) (*types.Block, error) {
	var block = &types.Block{}
	err := json.Unmarshal(blockByte, block)
	if err != nil {
		return nil, err
	} else {
		return block, nil
	}
}
