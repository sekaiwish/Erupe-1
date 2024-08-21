package mhfpacket

import (
	"errors"

	"erupe-ce/common/byteframe"
	"erupe-ce/network"
	"erupe-ce/network/clientctx"
)

type OperateGuildMemberAction uint8

const (
	OperateGuildMemberAccept = iota + 1
	OperateGuildMemberReject
	OperateGuildMemberKick
)

// MsgMhfOperateGuildMember represents the MSG_MHF_OPERATE_GUILD_MEMBER
type MsgMhfOperateGuildMember struct {
	AckHandle uint32
	GuildID   uint32
	CharID    uint32
	Action    uint8
}

// Opcode returns the ID associated with this packet type.
func (m *MsgMhfOperateGuildMember) Opcode() network.PacketID {
	return network.MSG_MHF_OPERATE_GUILD_MEMBER
}

// Parse parses the packet from binary
func (m *MsgMhfOperateGuildMember) Parse(bf *byteframe.ByteFrame, ctx *clientctx.ClientContext) error {
	m.AckHandle = bf.ReadUint32()
	m.GuildID = bf.ReadUint32()
	m.CharID = bf.ReadUint32()
	m.Action = bf.ReadUint8()
	bf.ReadUint8()  // Zeroed
	bf.ReadUint16() // Zeroed
	return nil
}

// Build builds a binary packet from the current data.
func (m *MsgMhfOperateGuildMember) Build(bf *byteframe.ByteFrame, ctx *clientctx.ClientContext) error {
	return errors.New("NOT IMPLEMENTED")
}
