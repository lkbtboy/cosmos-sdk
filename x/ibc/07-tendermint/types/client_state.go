package types

import (
	"errors"
	"fmt"
	"time"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	clientexported "github.com/cosmos/cosmos-sdk/x/ibc/02-client/exported"
	clienttypes "github.com/cosmos/cosmos-sdk/x/ibc/02-client/types"
	connectionexported "github.com/cosmos/cosmos-sdk/x/ibc/03-connection/exported"
	channelexported "github.com/cosmos/cosmos-sdk/x/ibc/04-channel/exported"
	commitmentexported "github.com/cosmos/cosmos-sdk/x/ibc/23-commitment/exported"
	commitmenttypes "github.com/cosmos/cosmos-sdk/x/ibc/23-commitment/types"
	ibctypes "github.com/cosmos/cosmos-sdk/x/ibc/types"
)

var _ clientexported.ClientState = ClientState{}

// ClientState from Tendermint tracks the current validator set, latest height,
// and a possible frozen height.
type ClientState struct {
	// Client ID
	ID string `json:"id" yaml:"id"`
	// Duration of the period since the LastestTimestamp during which the
	// submitted headers are valid for upgrade
	TrustingPeriod time.Duration `json:"trusting_period" yaml:"trusting_period"`
	// Duration of the staking unbonding period
	UnbondingPeriod time.Duration `json:"unbonding_period" yaml:"unbonding_period"`
	// Block height when the client was frozen due to a misbehaviour
	FrozenHeight uint64 `json:"frozen_height" yaml:"frozen_height"`
	// Last Header that was stored by client
	LastHeader Header `json:"last_header" yaml:"last_header"`
}

// InitializeFromMsg creates a tendermint client state from a CreateClientMsg
func InitializeFromMsg(
	msg MsgCreateClient,
) (ClientState, error) {
	return Initialize(msg.GetClientID(), msg.TrustingPeriod, msg.UnbondingPeriod, msg.Header)
}

// Initialize creates a client state and validates its contents, checking that
// the provided consensus state is from the same client type.
func Initialize(
	id string, trustingPeriod, ubdPeriod time.Duration,
	header Header,
) (ClientState, error) {
	if trustingPeriod >= ubdPeriod {
		return ClientState{}, errors.New("trusting period should be < unbonding period")
	}

	clientState := NewClientState(
		id, trustingPeriod, ubdPeriod, header,
	)
	return clientState, nil
}

// NewClientState creates a new ClientState instance
func NewClientState(
	id string, trustingPeriod, ubdPeriod time.Duration,
	header Header,
) ClientState {
	return ClientState{
		ID:              id,
		TrustingPeriod:  trustingPeriod,
		UnbondingPeriod: ubdPeriod,
		LastHeader:      header,
		FrozenHeight:    0,
	}
}

// GetID returns the tendermint client state identifier.
func (cs ClientState) GetID() string {
	return cs.ID
}

// GetChainID returns the chain-id from the last header
func (cs ClientState) GetChainID() string {
	return cs.LastHeader.ChainID
}

// ClientType is tendermint.
func (cs ClientState) ClientType() clientexported.ClientType {
	return clientexported.Tendermint
}

// GetLatestHeight returns latest block height.
func (cs ClientState) GetLatestHeight() uint64 {
	return uint64(cs.LastHeader.Height)
}

// GetLatestTimestamp returns latest block time.
func (cs ClientState) GetLatestTimestamp() time.Time {
	return cs.LastHeader.Time
}

// IsFrozen returns true if the frozen height has been set.
func (cs ClientState) IsFrozen() bool {
	return cs.FrozenHeight != 0
}

// VerifyClientConsensusState verifies a proof of the consensus state of the
// Tendermint client stored on the target machine.
func (cs ClientState) VerifyClientConsensusState(
	cdc *codec.Codec,
	provingRoot commitmentexported.Root,
	height uint64,
	counterpartyClientIdentifier string,
	consensusHeight uint64,
	prefix commitmentexported.Prefix,
	proof commitmentexported.Proof,
	consensusState clientexported.ConsensusState,
) error {
	clientPrefixedPath := "clients/" + counterpartyClientIdentifier + "/" + ibctypes.ConsensusStatePath(consensusHeight)
	path, err := commitmenttypes.ApplyPrefix(prefix, clientPrefixedPath)
	if err != nil {
		return err
	}

	if err := validateVerificationArgs(cs, height, proof, consensusState); err != nil {
		return err
	}

	bz, err := cdc.MarshalBinaryBare(consensusState)
	if err != nil {
		return err
	}

	if err := proof.VerifyMembership(provingRoot, path, bz); err != nil {
		return sdkerrors.Wrap(clienttypes.ErrFailedClientConsensusStateVerification, err.Error())
	}

	return nil
}

// VerifyConnectionState verifies a proof of the connection state of the
// specified connection end stored on the target machine.
func (cs ClientState) VerifyConnectionState(
	cdc *codec.Codec,
	height uint64,
	prefix commitmentexported.Prefix,
	proof commitmentexported.Proof,
	connectionID string,
	connectionEnd connectionexported.ConnectionI,
	consensusState clientexported.ConsensusState,
) error {
	path, err := commitmenttypes.ApplyPrefix(prefix, ibctypes.ConnectionPath(connectionID))
	if err != nil {
		return err
	}

	if err := validateVerificationArgs(cs, height, proof, consensusState); err != nil {
		return err
	}

	bz, err := cdc.MarshalBinaryBare(connectionEnd)
	if err != nil {
		return err
	}

	if err := proof.VerifyMembership(consensusState.GetRoot(), path, bz); err != nil {
		return sdkerrors.Wrap(clienttypes.ErrFailedConnectionStateVerification, err.Error())
	}

	return nil
}

// VerifyChannelState verifies a proof of the channel state of the specified
// channel end, under the specified port, stored on the target machine.
func (cs ClientState) VerifyChannelState(
	cdc *codec.Codec,
	height uint64,
	prefix commitmentexported.Prefix,
	proof commitmentexported.Proof,
	portID,
	channelID string,
	channel channelexported.ChannelI,
	consensusState clientexported.ConsensusState,
) error {
	path, err := commitmenttypes.ApplyPrefix(prefix, ibctypes.ChannelPath(portID, channelID))
	if err != nil {
		return err
	}

	if err := validateVerificationArgs(cs, height, proof, consensusState); err != nil {
		return err
	}

	bz, err := cdc.MarshalBinaryBare(channel)
	if err != nil {
		return err
	}

	if err := proof.VerifyMembership(consensusState.GetRoot(), path, bz); err != nil {
		return sdkerrors.Wrap(clienttypes.ErrFailedChannelStateVerification, err.Error())
	}

	return nil
}

// VerifyPacketCommitment verifies a proof of an outgoing packet commitment at
// the specified port, specified channel, and specified sequence.
func (cs ClientState) VerifyPacketCommitment(
	height uint64,
	prefix commitmentexported.Prefix,
	proof commitmentexported.Proof,
	portID,
	channelID string,
	sequence uint64,
	commitmentBytes []byte,
	consensusState clientexported.ConsensusState,
) error {
	path, err := commitmenttypes.ApplyPrefix(prefix, ibctypes.PacketCommitmentPath(portID, channelID, sequence))
	if err != nil {
		return err
	}

	if err := validateVerificationArgs(cs, height, proof, consensusState); err != nil {
		return err
	}

	if err := proof.VerifyMembership(consensusState.GetRoot(), path, commitmentBytes); err != nil {
		return sdkerrors.Wrap(clienttypes.ErrFailedPacketCommitmentVerification, err.Error())
	}

	return nil
}

// VerifyPacketAcknowledgement verifies a proof of an incoming packet
// acknowledgement at the specified port, specified channel, and specified sequence.
func (cs ClientState) VerifyPacketAcknowledgement(
	height uint64,
	prefix commitmentexported.Prefix,
	proof commitmentexported.Proof,
	portID,
	channelID string,
	sequence uint64,
	acknowledgement []byte,
	consensusState clientexported.ConsensusState,
) error {
	path, err := commitmenttypes.ApplyPrefix(prefix, ibctypes.PacketAcknowledgementPath(portID, channelID, sequence))
	if err != nil {
		return err
	}

	if err := validateVerificationArgs(cs, height, proof, consensusState); err != nil {
		return err
	}

	if err := proof.VerifyMembership(consensusState.GetRoot(), path, acknowledgement); err != nil {
		return sdkerrors.Wrap(clienttypes.ErrFailedPacketAckVerification, err.Error())
	}

	return nil
}

// VerifyPacketAcknowledgementAbsence verifies a proof of the absence of an
// incoming packet acknowledgement at the specified port, specified channel, and
// specified sequence.
func (cs ClientState) VerifyPacketAcknowledgementAbsence(
	height uint64,
	prefix commitmentexported.Prefix,
	proof commitmentexported.Proof,
	portID,
	channelID string,
	sequence uint64,
	consensusState clientexported.ConsensusState,
) error {
	path, err := commitmenttypes.ApplyPrefix(prefix, ibctypes.PacketAcknowledgementPath(portID, channelID, sequence))
	if err != nil {
		return err
	}

	if err := validateVerificationArgs(cs, height, proof, consensusState); err != nil {
		return err
	}

	if err := proof.VerifyNonMembership(consensusState.GetRoot(), path); err != nil {
		return sdkerrors.Wrap(clienttypes.ErrFailedPacketAckAbsenceVerification, err.Error())
	}

	return nil
}

// VerifyNextSequenceRecv verifies a proof of the next sequence number to be
// received of the specified channel at the specified port.
func (cs ClientState) VerifyNextSequenceRecv(
	height uint64,
	prefix commitmentexported.Prefix,
	proof commitmentexported.Proof,
	portID,
	channelID string,
	nextSequenceRecv uint64,
	consensusState clientexported.ConsensusState,
) error {
	path, err := commitmenttypes.ApplyPrefix(prefix, ibctypes.NextSequenceRecvPath(portID, channelID))
	if err != nil {
		return err
	}

	if err := validateVerificationArgs(cs, height, proof, consensusState); err != nil {
		return err
	}

	bz := sdk.Uint64ToBigEndian(nextSequenceRecv)

	if err := proof.VerifyMembership(consensusState.GetRoot(), path, bz); err != nil {
		return sdkerrors.Wrap(clienttypes.ErrFailedNextSeqRecvVerification, err.Error())
	}

	return nil
}

// validateVerificationArgs perfoms the basic checks on the arguments that are
// shared between the verification functions.
func validateVerificationArgs(
	cs ClientState,
	height uint64,
	proof commitmentexported.Proof,
	consensusState clientexported.ConsensusState,
) error {
	if cs.GetLatestHeight() < height {
		return sdkerrors.Wrap(
			ibctypes.ErrInvalidHeight,
			fmt.Sprintf("client state (%s) height < proof height (%d < %d)", cs.ID, cs.GetLatestHeight(), height),
		)
	}

	if cs.IsFrozen() && cs.FrozenHeight <= height {
		return clienttypes.ErrClientFrozen
	}

	if proof == nil {
		return sdkerrors.Wrap(commitmenttypes.ErrInvalidProof, "proof cannot be empty")
	}

	if consensusState == nil {
		return sdkerrors.Wrap(clienttypes.ErrInvalidConsensus, "consensus state cannot be empty")
	}

	return nil
}
