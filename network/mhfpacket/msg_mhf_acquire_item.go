package mhfpacket

import (
	"errors"

	"erupe-ce/common/byteframe"
	"erupe-ce/network"
	"erupe-ce/network/clientctx"
)

// MsgMhfAcquireItem represents the MSG_MHF_ACQUIRE_ITEM
type MsgMhfAcquireItem struct {
	AckHandle uint32
	RewardIDs []uint32
}

// Opcode returns the ID associated with this packet type.
func (m *MsgMhfAcquireItem) Opcode() network.PacketID {
	return network.MSG_MHF_ACQUIRE_ITEM
}

// Parse parses the packet from binary
func (m *MsgMhfAcquireItem) Parse(bf *byteframe.ByteFrame, ctx *clientctx.ClientContext) error {
	m.AckHandle = bf.ReadUint32()
	bf.ReadUint16() // Zeroed
	ids := bf.ReadUint16()
	for i := uint16(0); i < ids; i++ {
		m.RewardIDs = append(m.RewardIDs, bf.ReadUint32())
	}
	return nil
}

// Build builds a binary packet from the current data.
func (m *MsgMhfAcquireItem) Build(bf *byteframe.ByteFrame, ctx *clientctx.ClientContext) error {
	return errors.New("NOT IMPLEMENTED")
}
