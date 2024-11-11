package mhfpacket

import (
	"erupe-ce/common/byteframe"
	"erupe-ce/network"
	"erupe-ce/network/clientctx"
)

// MsgMhfEnumerateCampaign represents the MSG_MHF_ENUMERATE_CAMPAIGN
type MsgMhfEnumerateCampaign struct {
	AckHandle    uint32
	NullPadding1 uint16 // 0 in z2
	NullPadding2 uint16 // 0 in z2
}

// Opcode returns the ID associated with this packet type.
func (m *MsgMhfEnumerateCampaign) Opcode() network.PacketID {
	return network.MSG_MHF_ENUMERATE_CAMPAIGN
}

// Parse parses the packet from binary
func (m *MsgMhfEnumerateCampaign) Parse(bf *byteframe.ByteFrame, ctx *clientctx.ClientContext) error {
	m.AckHandle = bf.ReadUint32()
	m.NullPadding1 = bf.ReadUint16()
	m.NullPadding2 = bf.ReadUint16()
	return nil
}

// Build builds a binary packet from the current data.
func (m *MsgMhfEnumerateCampaign) Build(bf *byteframe.ByteFrame, ctx *clientctx.ClientContext) error {
	bf.WriteUint32(m.AckHandle)
	bf.WriteUint16(m.NullPadding1)
	bf.WriteUint16(m.NullPadding2)
	return nil
}
