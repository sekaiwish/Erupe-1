package mhfpacket

import (
	"errors"

	"erupe-ce/common/byteframe"
	"erupe-ce/network"
	"erupe-ce/network/clientctx"
)

// MsgMhfEnumerateItem represents the MSG_MHF_ENUMERATE_ITEM
type MsgMhfEnumerateItem struct {
	AckHandle  uint32
	CampaignID uint32
}

// Opcode returns the ID associated with this packet type.
func (m *MsgMhfEnumerateItem) Opcode() network.PacketID {
	return network.MSG_MHF_ENUMERATE_ITEM
}

// Parse parses the packet from binary
func (m *MsgMhfEnumerateItem) Parse(bf *byteframe.ByteFrame, ctx *clientctx.ClientContext) error {
	m.AckHandle = bf.ReadUint32()
	bf.ReadUint16() // Zeroed
	bf.ReadUint16() // Always 2
	m.CampaignID = bf.ReadUint32()
	return nil
}

// Build builds a binary packet from the current data.
func (m *MsgMhfEnumerateItem) Build(bf *byteframe.ByteFrame, ctx *clientctx.ClientContext) error {
	return errors.New("NOT IMPLEMENTED")
}
