// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
	"github.com/ava-labs/avalanchego/vms/platformvm/validators"
)

var (
	_ Manager = (*manager)(nil)

	ErrChainNotSynced = errors.New("chain not synced")
)

type Manager interface {
	state.Versions

	// Returns the ID of the most recently accepted block.
	LastAccepted() ids.ID

	SetPreference(blkID ids.ID) (updated bool)
	Preferred() ids.ID

	GetBlock(blkID ids.ID) (snowman.Block, error)
	GetStatelessBlock(blkID ids.ID) (block.Block, error)
	NewBlock(block.Block) snowman.Block

	// VerifyTx verifies that the transaction can be issued based on the currently
	// preferred state. This should *not* be used to verify transactions in a block.
	VerifyTx(tx *txs.Tx) error
}

func NewManager(
	mempool mempool.Mempool,
	metrics metrics.Metrics,
	s state.State,
	txExecutorBackend *executor.Backend,
	validatorManager validators.Manager,
) Manager {
	lastAccepted := s.GetLastAccepted()
	backend := &backend{
		Mempool:      mempool,
		lastAccepted: lastAccepted,
		state:        s,
		ctx:          txExecutorBackend.Ctx,
		blkIDToState: map[ids.ID]*blockState{},
	}

	return &manager{
		backend: backend,
		verifier: &verifier{
			backend:           backend,
			txExecutorBackend: txExecutorBackend,
		},
		acceptor: &acceptor{
			backend:      backend,
			metrics:      metrics,
			validators:   validatorManager,
			bootstrapped: txExecutorBackend.Bootstrapped,
		},
		rejector: &rejector{
			backend:         backend,
			addTxsToMempool: !txExecutorBackend.Config.PartialSyncPrimaryNetwork,
		},
		preferred:         lastAccepted,
		txExecutorBackend: txExecutorBackend,
	}
}

type manager struct {
	*backend
	verifier block.Visitor
	acceptor block.Visitor
	rejector block.Visitor

	preferred         ids.ID
	txExecutorBackend *executor.Backend
}

func (m *manager) GetBlock(blkID ids.ID) (snowman.Block, error) {
	blk, err := m.backend.GetBlock(blkID)
	if err != nil {
		return nil, err
	}
	return m.NewBlock(blk), nil
}

func (m *manager) GetStatelessBlock(blkID ids.ID) (block.Block, error) {
	return m.backend.GetBlock(blkID)
}

func (m *manager) NewBlock(blk block.Block) snowman.Block {
	return &Block{
		manager: m,
		Block:   blk,
	}
}

func (m *manager) SetPreference(blockID ids.ID) (updated bool) {
	updated = m.preferred == blockID
	m.preferred = blockID
	return updated
}

func (m *manager) Preferred() ids.ID {
	return m.preferred
}

func (m *manager) VerifyTx(tx *txs.Tx) error {
	if !m.txExecutorBackend.Bootstrapped.Get() {
		return ErrChainNotSynced
	}

	return tx.Unsigned.Visit(&executor.MempoolTxVerifier{
		Backend:       m.txExecutorBackend,
		ParentID:      m.preferred,
		StateVersions: m,
		Tx:            tx,
	})
}
