package mhfpacket

import (
	"errors"
	"fmt"

	"erupe-ce/common/byteframe"
	"erupe-ce/network"
	"erupe-ce/network/clientctx"
)

// MsgMhfReadLastWeekBeatRanking represents the MSG_MHF_READ_LAST_WEEK_BEAT_RANKING
type MsgMhfReadLastWeekBeatRanking struct {
	AckHandle    uint32
	Unk0         uint32
	EarthMonster int32
}

// Opcode returns the ID associated with this packet type.
func (m *MsgMhfReadLastWeekBeatRanking) Opcode() network.PacketID {
	return network.MSG_MHF_READ_LAST_WEEK_BEAT_RANKING
}

// Parse parses the packet from binary
func (m *MsgMhfReadLastWeekBeatRanking) Parse(bf *byteframe.ByteFrame, ctx *clientctx.ClientContext) error {
	m.AckHandle = bf.ReadUint32()
	m.Unk0 = bf.ReadUint32()
	m.EarthMonster = bf.ReadInt32()

	fmt.Printf("MsgMhfGetFixedSeibatuRankingTable: Unk0:[%d] EarthMonster:[%d] \n\n", m.Unk0, m.EarthMonster)

	return nil
}

// Build builds a binary packet from the current data.
func (m *MsgMhfReadLastWeekBeatRanking) Build(bf *byteframe.ByteFrame, ctx *clientctx.ClientContext) error {
	return errors.New("NOT IMPLEMENTED")
}
