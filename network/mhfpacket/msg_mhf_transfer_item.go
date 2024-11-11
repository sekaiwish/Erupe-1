package mhfpacket

import (
	"errors"

	"erupe-ce/common/byteframe"
	"erupe-ce/network"
	"erupe-ce/network/clientctx"
)

// MsgMhfTransferItem represents the MSG_MHF_TRANSFER_ITEM
type MsgMhfTransferItem struct {
	AckHandle uint32
	QuestID   uint32
	ItemType  uint8
	Quantity  uint16
}

// Opcode returns the ID associated with this packet type.
func (m *MsgMhfTransferItem) Opcode() network.PacketID {
	return network.MSG_MHF_TRANSFER_ITEM
}

// Parse parses the packet from binary
func (m *MsgMhfTransferItem) Parse(bf *byteframe.ByteFrame, ctx *clientctx.ClientContext) error {
	m.AckHandle = bf.ReadUint32()
	m.QuestID = bf.ReadUint32()
	m.ItemType = bf.ReadUint8()
	bf.ReadUint8() // Zeroed
	m.Quantity = bf.ReadUint16()
	return nil
}

// Build builds a binary packet from the current data.
func (m *MsgMhfTransferItem) Build(bf *byteframe.ByteFrame, ctx *clientctx.ClientContext) error {
	return errors.New("NOT IMPLEMENTED")
}
