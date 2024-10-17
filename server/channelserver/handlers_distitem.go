package channelserver

import (
	"erupe-ce/config"
	"erupe-ce/internal/model"
	"erupe-ce/network/mhfpacket"
	"erupe-ce/utils/byteframe"
	"erupe-ce/utils/db"
	ps "erupe-ce/utils/pascalstring"
	"fmt"

	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
)

func handleMsgMhfEnumerateDistItem(s *Session, db *sqlx.DB, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfEnumerateDistItem)

	var itemDists []model.Distribution
	bf := byteframe.NewByteFrame()

	rows, err := db.Queryx(`
		SELECT d.id, event_name, description, COALESCE(rights, 0) AS rights, COALESCE(selection, false) AS selection, times_acceptable,
		COALESCE(min_hr, -1) AS min_hr, COALESCE(max_hr, -1) AS max_hr,
		COALESCE(min_sr, -1) AS min_sr, COALESCE(max_sr, -1) AS max_sr,
		COALESCE(min_gr, -1) AS min_gr, COALESCE(max_gr, -1) AS max_gr,
		(
    		SELECT count(*) FROM distributions_accepted da
    		WHERE d.id = da.distribution_id AND da.character_id = $1
		) AS times_accepted,
		COALESCE(deadline, TO_TIMESTAMP(0)) AS deadline
		FROM distribution d
		WHERE character_id = $1 AND type = $2 OR character_id IS NULL AND type = $2 ORDER BY id DESC
	`, s.CharID, pkt.DistType)

	if err == nil {
		var itemDist model.Distribution
		for rows.Next() {
			err = rows.StructScan(&itemDist)
			if err != nil {
				continue
			}
			itemDists = append(itemDists, itemDist)
		}
	}

	bf.WriteUint16(uint16(len(itemDists)))
	for _, dist := range itemDists {
		bf.WriteUint32(dist.ID)
		bf.WriteUint32(uint32(dist.Deadline.Unix()))
		bf.WriteUint32(dist.Rights)
		bf.WriteUint16(dist.TimesAcceptable)
		bf.WriteUint16(dist.TimesAccepted)
		if config.GetConfig().ClientID >= config.G9 {
			bf.WriteUint16(0) // Unk
		}
		bf.WriteInt16(dist.MinHR)
		bf.WriteInt16(dist.MaxHR)
		bf.WriteInt16(dist.MinSR)
		bf.WriteInt16(dist.MaxSR)
		bf.WriteInt16(dist.MinGR)
		bf.WriteInt16(dist.MaxGR)
		if config.GetConfig().ClientID >= config.G7 {
			bf.WriteUint8(0) // Unk
		}
		if config.GetConfig().ClientID >= config.G6 {
			bf.WriteUint16(0) // Unk
		}
		if config.GetConfig().ClientID >= config.G8 {
			if dist.Selection {
				bf.WriteUint8(2) // Selection
			} else {
				bf.WriteUint8(0)
			}
		}
		if config.GetConfig().ClientID >= config.G7 {
			bf.WriteUint16(0) // Unk
			bf.WriteUint16(0) // Unk
		}
		if config.GetConfig().ClientID >= config.G10 {
			bf.WriteUint8(0) // Unk
		}
		ps.Uint8(bf, dist.EventName, true)
		k := 6
		if config.GetConfig().ClientID >= config.G8 {
			k = 13
		}
		for i := 0; i < 6; i++ {
			for j := 0; j < k; j++ {
				bf.WriteUint8(0)
				bf.WriteUint32(0)
			}
		}
		if config.GetConfig().ClientID >= config.Z2 {
			i := uint8(0)
			bf.WriteUint8(i)
			if i <= 10 {
				for j := uint8(0); j < i; j++ {
					bf.WriteUint32(0)
					bf.WriteUint32(0)
					bf.WriteUint32(0)
				}
			}
		}
	}
	s.DoAckBufSucceed(pkt.AckHandle, bf.Data())
}

func getDistributionItems(s *Session, i uint32) []model.DistributionItem {
	db, err := db.GetDB()
	if err != nil {
		s.Logger.Fatal(fmt.Sprintf("Failed to get database instance: %s", err))
	}
	var distItems []model.DistributionItem
	rows, err := db.Queryx(`SELECT id, item_type, COALESCE(item_id, 0) AS item_id, COALESCE(quantity, 0) AS quantity FROM distribution_items WHERE distribution_id=$1`, i)
	if err == nil {
		var distItem model.DistributionItem
		for rows.Next() {
			err = rows.StructScan(&distItem)
			if err != nil {
				continue
			}
			distItems = append(distItems, distItem)
		}
	}
	return distItems
}

func handleMsgMhfApplyDistItem(s *Session, db *sqlx.DB, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfApplyDistItem)
	bf := byteframe.NewByteFrame()
	bf.WriteUint32(pkt.DistributionID)
	distItems := getDistributionItems(s, pkt.DistributionID)
	bf.WriteUint16(uint16(len(distItems)))
	for _, item := range distItems {
		bf.WriteUint8(item.ItemType)
		bf.WriteUint32(item.ItemID)
		bf.WriteUint32(item.Quantity)
		if config.GetConfig().ClientID >= config.G8 {
			bf.WriteUint32(item.ID)
		}
	}
	s.DoAckBufSucceed(pkt.AckHandle, bf.Data())
}

func handleMsgMhfAcquireDistItem(s *Session, db *sqlx.DB, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfAcquireDistItem)

	if pkt.DistributionID > 0 {
		_, err := db.Exec(`INSERT INTO public.distributions_accepted VALUES ($1, $2)`, pkt.DistributionID, s.CharID)
		if err == nil {
			distItems := getDistributionItems(s, pkt.DistributionID)
			for _, item := range distItems {
				switch item.ItemType {
				case 17:
					_ = addPointNetcafe(s, int(item.Quantity))
				case 19:
					db.Exec("UPDATE users u SET gacha_premium=gacha_premium+$1 WHERE u.id=(SELECT c.user_id FROM characters c WHERE c.id=$2)", item.Quantity, s.CharID)
				case 20:
					db.Exec("UPDATE users u SET gacha_trial=gacha_trial+$1 WHERE u.id=(SELECT c.user_id FROM characters c WHERE c.id=$2)", item.Quantity, s.CharID)
				case 21:
					db.Exec("UPDATE users u SET frontier_points=frontier_points+$1 WHERE u.id=(SELECT c.user_id FROM characters c WHERE c.id=$2)", item.Quantity, s.CharID)
				case 23:
					saveData, err := GetCharacterSaveData(s, s.CharID)
					if err == nil {
						saveData.RP += uint16(item.Quantity)
						saveData.Save(s)
					}
				}
			}
		}
	}
	s.DoAckSimpleSucceed(pkt.AckHandle, make([]byte, 4))
}

func handleMsgMhfGetDistDescription(s *Session, db *sqlx.DB, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfGetDistDescription)

	var desc string
	err := db.QueryRow("SELECT description FROM distribution WHERE id = $1", pkt.DistributionID).Scan(&desc)
	if err != nil {
		s.Logger.Error("Error parsing item distribution description", zap.Error(err))
		s.DoAckBufSucceed(pkt.AckHandle, make([]byte, 4))
		return
	}
	bf := byteframe.NewByteFrame()
	ps.Uint16(bf, desc, true)
	ps.Uint16(bf, "", false)
	s.DoAckBufSucceed(pkt.AckHandle, bf.Data())
}
