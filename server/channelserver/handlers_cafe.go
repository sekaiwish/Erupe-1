package channelserver

import (
	"erupe-ce/common/byteframe"
	ps "erupe-ce/common/pascalstring"
	"erupe-ce/network/mhfpacket"
	"go.uber.org/zap"
	"time"
)

func handleMsgMhfAcquireCafeItem(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfAcquireCafeItem)
	var netcafePoints uint32
	err := s.server.db.QueryRow("UPDATE characters SET netcafe_points = netcafe_points - $1 WHERE id = $2 RETURNING netcafe_points", pkt.PointCost, s.charID).Scan(&netcafePoints)
	if err != nil {
		s.logger.Fatal("Failed to get netcafe points from db", zap.Error(err))
	}
	resp := byteframe.NewByteFrame()
	resp.WriteUint32(netcafePoints)
	doAckSimpleSucceed(s, pkt.AckHandle, resp.Data())
}

func handleMsgMhfUpdateCafepoint(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfUpdateCafepoint)
	var netcafePoints uint32
	err := s.server.db.QueryRow("SELECT COALESCE(netcafe_points, 0) FROM characters WHERE id = $1", s.charID).Scan(&netcafePoints)
	if err != nil {
		s.logger.Fatal("Failed to get netcate points from db", zap.Error(err))
	}
	resp := byteframe.NewByteFrame()
	resp.WriteUint32(0)
	resp.WriteUint32(netcafePoints)
	doAckSimpleSucceed(s, pkt.AckHandle, resp.Data())
}

func handleMsgMhfCheckDailyCafepoint(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfCheckDailyCafepoint)

	// I am not sure exactly what this does, but all responses I have seen include this exact sequence of bytes
	// 1 daily, 5 daily halk pots, 3 point boosted quests, also adds 5 netcafe points but not sent to client
	// available once after midday every day

	// get next midday
	var t = Time_static()
	year, month, day := t.Date()
	midday := time.Date(year, month, day, 12, 0, 0, 0, t.Location())
	if t.After(midday) {
		midday = midday.Add(24 * time.Hour)
	}

	// get time after which daily claiming would be valid from db
	var dailyTime time.Time
	err := s.server.db.QueryRow("SELECT COALESCE(daily_time, $2) FROM characters WHERE id = $1", s.charID, time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)).Scan(&dailyTime)
	if err != nil {
		s.logger.Fatal("Failed to get daily_time savedata from db", zap.Error(err))
	}

	if t.After(dailyTime) {
		// +5 netcafe points and setting next valid window
		_, err := s.server.db.Exec("UPDATE characters SET daily_time=$1, netcafe_points=netcafe_points::int + 5 WHERE id=$2", midday, s.charID)
		if err != nil {
			s.logger.Fatal("Failed to update daily_time and netcafe_points savedata in db", zap.Error(err))
		}
		doAckBufSucceed(s, pkt.AckHandle, []byte{0x01, 0x00, 0x00, 0x00, 0x05, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00, 0x01})
	} else {
		doAckBufSucceed(s, pkt.AckHandle, []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	}
}

func handleMsgMhfGetCafeDuration(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfGetCafeDuration)
	bf := byteframe.NewByteFrame()

	var cafeTime uint32
	err := s.server.db.QueryRow("SELECT cafe_time FROM characters WHERE id = $1", s.charID).Scan(&cafeTime)
	if err != nil {
		panic(err)
	}
	cafeTime = uint32(Time_Current_Adjusted().Unix()) - uint32(s.sessionStart) + cafeTime
	bf.WriteUint32(cafeTime) // Total cafe time
	bf.WriteUint16(0)
	ps.Uint16(bf, "Resets at next maintenance", true)
	doAckBufSucceed(s, pkt.AckHandle, bf.Data())
}

type CafeBonus struct {
	ID       uint32 `db:"id"`
	Seconds  uint32 `db:"time_req"`
	ItemType uint32 `db:"item_type"`
	ItemID   uint32 `db:"item_id"`
	Quantity uint32 `db:"quantity"`
	Claimed  bool   `db:"claimed"`
}

func handleMsgMhfGetCafeDurationBonusInfo(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfGetCafeDurationBonusInfo)
	bf := byteframe.NewByteFrame()

	var count uint32
	rows, err := s.server.db.Queryx(`
	SELECT cb.id, seconds_req, item_type, item_id, quantity,
	(
		SELECT count(*)
		FROM cafe_accepted ca
		WHERE cb.id = ca.cafe_id AND ca.character_id = $1
	)::int::bool AS claimed
	FROM cafebonus cb ORDER BY id ASC;`, s.charID)
	if err != nil {
		s.logger.Error("Error getting cafebonus", zap.Error(err))
		doAckBufSucceed(s, pkt.AckHandle, make([]byte, 4))
	} else {
		for rows.Next() {
			count++
			cafeBonus := &CafeBonus{}
			err = rows.StructScan(&cafeBonus)
			if err != nil {
				s.logger.Error("Error scanning cafebonus", zap.Error(err))
			}
			bf.WriteUint32(cafeBonus.Seconds)
			bf.WriteUint32(0) // Unk
			bf.WriteUint32(cafeBonus.ItemID)
			bf.WriteUint32(cafeBonus.Quantity)
			bf.WriteBool(cafeBonus.Claimed)
		}
		resp := byteframe.NewByteFrame()
		resp.WriteUint32(0)
		resp.WriteUint32(uint32(time.Now().Unix()))
		resp.WriteUint32(count)
		resp.WriteBytes(bf.Data())
		doAckBufSucceed(s, pkt.AckHandle, resp.Data())
	}
}

func handleMsgMhfReceiveCafeDurationBonus(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfReceiveCafeDurationBonus)
	bf := byteframe.NewByteFrame()

	row := s.server.db.QueryRowx(`
	SELECT c.id, seconds_req, item_type, item_id, quantity
	FROM cafebonus c
	WHERE (
		SELECT count(*)
		FROM cafe_accepted ca
		WHERE c.id = ca.cafe_id AND ca.character_id = $1
	) < 1 AND (
		SELECT ch.cafe_time + $2
		FROM characters ch
		WHERE ch.id = $1 
	) >= seconds_req LIMIT 1;`, s.charID, Time_Current_Adjusted().Unix()-s.sessionStart)
	cafeBonus := &CafeBonus{}
	err := row.StructScan(cafeBonus)
	if err != nil {
		doAckBufSucceed(s, pkt.AckHandle, make([]byte, 8))
		return
	}
	bf.WriteUint32(1)
	bf.WriteUint32(cafeBonus.ID)
	bf.WriteUint32(cafeBonus.ItemType)
	bf.WriteUint32(cafeBonus.ItemID)
	bf.WriteUint32(cafeBonus.Quantity)
	doAckBufSucceed(s, pkt.AckHandle, bf.Data())
}

func handleMsgMhfPostCafeDurationBonusReceived(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfPostCafeDurationBonusReceived)

	_, err := s.server.db.Exec("INSERT INTO public.cafe_accepted VALUES ($1, $2)", pkt.CafeBonusID, s.charID)
	if err != nil {
		s.logger.Error("Error updating accepted CafeBonus count", zap.Error(err))
	}

	doAckSimpleSucceed(s, pkt.AckHandle, make([]byte, 4))
}

func handleMsgMhfStartBoostTime(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfStartBoostTime)
	doAckSimpleSucceed(s, pkt.AckHandle, make([]byte, 4))
}
