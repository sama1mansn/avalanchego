// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	_ ValidatorSet = (*Validators)(nil)
	_ NodeSampler  = (*Validators)(nil)
)

type ValidatorSet interface {
	Has(ctx context.Context, nodeID ids.NodeID) bool // TODO return error
}

func NewValidators(
	peers *Peers,
	log logging.Logger,
	subnetID ids.ID,
	validators validators.State,
	maxValidatorSetStaleness time.Duration,
) *Validators {
	return &Validators{
		peers:                    peers,
		log:                      log,
		subnetID:                 subnetID,
		validators:               validators,
		maxValidatorSetStaleness: maxValidatorSetStaleness,
	}
}

// Validators contains a set of nodes that are staking.
type Validators struct {
	peers      *Peers
	log        logging.Logger
	subnetID   ids.ID
	validators validators.State

	lock                     sync.Mutex
	validatorIDs             set.SampleableSet[ids.NodeID]
	lastUpdated              time.Time
	maxValidatorSetStaleness time.Duration
}

func (v *Validators) refresh(ctx context.Context) {
	if time.Since(v.lastUpdated) < v.maxValidatorSetStaleness {
		return
	}

	v.validatorIDs.Clear()

	height, err := v.validators.GetCurrentHeight(ctx)
	if err != nil {
		v.log.Warn("failed to get current height", zap.Error(err))
		return
	}
	validatorSet, err := v.validators.GetValidatorSet(ctx, height, v.subnetID)
	if err != nil {
		v.log.Warn("failed to get validator set", zap.Error(err))
		return
	}

	for nodeID := range validatorSet {
		v.validatorIDs.Add(nodeID)
	}

	v.lastUpdated = time.Now()
}

// Sample returns a random sample of connected validators
func (v *Validators) Sample(ctx context.Context, limit int) []ids.NodeID {
	v.lock.Lock()
	defer v.lock.Unlock()

	v.refresh(ctx)

	validatorIDs := v.validatorIDs.Sample(limit)
	sampled := validatorIDs[:0]

	for _, validatorID := range validatorIDs {
		if !v.peers.has(validatorID) {
			continue
		}

		sampled = append(sampled, validatorID)
	}

	return sampled
}

// Has returns if nodeID is a connected validator
func (v *Validators) Has(ctx context.Context, nodeID ids.NodeID) bool {
	v.lock.Lock()
	defer v.lock.Unlock()

	v.refresh(ctx)

	return v.peers.has(nodeID) && v.validatorIDs.Contains(nodeID)
}
