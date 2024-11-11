package mhfpacket

import (
	"errors"
	"erupe-ce/common/bfutil"
	"erupe-ce/common/byteframe"
	"erupe-ce/network"
	"erupe-ce/network/clientctx"
)

// MsgMhfApplyCampaign represents the MSG_MHF_APPLY_CAMPAIGN
type MsgMhfApplyCampaign struct {
	AckHandle  uint32
	CampaignID uint32
	Code       string
}

// Opcode returns the ID associated with this packet type.
func (m *MsgMhfApplyCampaign) Opcode() network.PacketID {
	return network.MSG_MHF_APPLY_CAMPAIGN
}

// Parse parses the packet from binary
func (m *MsgMhfApplyCampaign) Parse(bf *byteframe.ByteFrame, ctx *clientctx.ClientContext) error {
	m.AckHandle = bf.ReadUint32()
	m.CampaignID = bf.ReadUint32()
	bf.ReadUint16() // Zeroed
	m.Code = string(bfutil.UpToNull(bf.ReadBytes(16)))
	return nil
}

// Build builds a binary packet from the current data.
func (m *MsgMhfApplyCampaign) Build(bf *byteframe.ByteFrame, ctx *clientctx.ClientContext) error {
	return errors.New("NOT IMPLEMENTED")
}
