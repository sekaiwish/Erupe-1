package channelserver

import (
	"erupe-ce/common/byteframe"
	ps "erupe-ce/common/pascalstring"
	"erupe-ce/common/stringsupport"
	_config "erupe-ce/config"
	"erupe-ce/network/mhfpacket"
	"time"
)

type CampaignEvent struct {
	ID           uint32    `db:"id"`
	MinHR        int16     `db:"min_hr"`
	MaxHR        int16     `db:"max_hr"`
	MinSR        int16     `db:"min_sr"`
	MaxSR        int16     `db:"max_sr"`
	MinGR        int16     `db:"min_gr"`
	MaxGR        int16     `db:"max_gr"`
	RewardType   uint16    `db:"reward_type"`
	Stamps       uint8     `db:"stamps"`
	Unk          uint8     `db:"unk"`
	BackgroundID uint16    `db:"background_id"`
	Start        time.Time `db:"start_time"`
	End          time.Time `db:"end_time"`
	Title        string    `db:"title"`
	Reward       string    `db:"reward"`
	Link         string    `db:"link"`
	Prefix       string    `db:"code_prefix"`
}

type CampaignCategory struct {
	ID          uint16 `db:"id"`
	Type        uint8  `db:"type"`
	Title       string `db:"title"`
	Description string `db:"description"`
}

type CampaignLink struct {
	CategoryID uint16 `db:"category_id"`
	CampaignID uint32 `db:"campaign_id"`
}

type CampaignReward struct {
	ID       uint32    `db:"id"`
	ItemType uint16    `db:"item_type"`
	Quantity uint16    `db:"quantity"`
	ItemID   uint16    `db:"item_id"`
	Deadline time.Time `db:"deadline"`
}

func handleMsgMhfEnumerateCampaign(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfEnumerateCampaign)
	bf := byteframe.NewByteFrame()

	var events []CampaignEvent
	var categories []CampaignCategory
	var campaignLinks []CampaignLink

	err := s.server.db.Select(&events, "SELECT id,min_hr,max_hr,min_sr,max_sr,min_gr,max_gr,reward_type,stamps,unk,background_id,start_time,end_time,title,reward,link,code_prefix FROM campaigns")
	if err != nil {
		doAckBufFail(s, pkt.AckHandle, make([]byte, 4))
		return
	}
	err = s.server.db.Select(&categories, "SELECT id, type, title, description FROM campaign_categories")
	if err != nil {
		doAckBufFail(s, pkt.AckHandle, make([]byte, 4))
		return
	}
	err = s.server.db.Select(&campaignLinks, "SELECT campaign_id, category_id FROM campaign_category_links")
	if err != nil {
		doAckBufFail(s, pkt.AckHandle, make([]byte, 4))
		return
	}
	if len(events) > 255 {
		bf.WriteUint8(255)
		bf.WriteUint16(uint16(len(events)))
	} else {
		bf.WriteUint8(uint8(len(events)))
	}
	for _, event := range events {
		bf.WriteUint32(event.ID)
		bf.WriteUint32(0)
		bf.WriteInt16(event.MinHR)
		bf.WriteInt16(event.MaxHR)
		bf.WriteInt16(event.MinSR)
		bf.WriteInt16(event.MaxSR)
		if _config.ErupeConfig.RealClientMode >= _config.G3 {
			bf.WriteInt16(event.MinGR)
			bf.WriteInt16(event.MaxGR)
		}
		bf.WriteUint16(event.RewardType)
		bf.WriteUint8(event.Stamps)
		bf.WriteUint8(event.Unk) // Related to stamp count
		bf.WriteUint16(event.BackgroundID)
		bf.WriteUint16(0)
		bf.WriteUint32(uint32(event.Start.Unix()))
		bf.WriteUint32(uint32(event.End.Unix()))
		if event.End.After(time.Now()) {
			bf.WriteBool(true)
		} else {
			bf.WriteBool(false)
		}
		ps.Uint8(bf, event.Title, true)
		ps.Uint8(bf, event.Reward, true)
		ps.Uint8(bf, "", false)
		ps.Uint8(bf, "", false)
		ps.Uint8(bf, event.Link, true)
	}

	if len(events) > 255 {
		bf.WriteUint8(255)
		bf.WriteUint16(uint16(len(events)))
	} else {
		bf.WriteUint8(uint8(len(events)))
	}
	for _, event := range events {
		bf.WriteUint32(event.ID)
		bf.WriteUint8(1) // Related to stamp count
		bf.WriteBytes([]byte(event.Prefix))
	}

	if len(categories) > 255 {
		bf.WriteUint8(255)
		bf.WriteUint16(uint16(len(categories)))
	} else {
		bf.WriteUint8(uint8(len(categories)))
	}
	for _, category := range categories {
		bf.WriteUint16(category.ID)
		bf.WriteUint8(category.Type)
		xTitle := stringsupport.UTF8ToSJIS(category.Title)
		xDescription := stringsupport.UTF8ToSJIS(category.Description)
		bf.WriteUint8(uint8(len(xTitle)))
		bf.WriteUint8(uint8(len(xDescription)))
		bf.WriteBytes(xTitle)
		bf.WriteBytes(xDescription)
	}

	if len(campaignLinks) > 255 {
		bf.WriteUint8(255)
		bf.WriteUint16(uint16(len(campaignLinks)))
	} else {
		bf.WriteUint8(uint8(len(campaignLinks)))
	}
	for _, link := range campaignLinks {
		bf.WriteUint16(link.CategoryID)
		bf.WriteUint32(link.CampaignID)
	}
	doAckBufSucceed(s, pkt.AckHandle, bf.Data())
}

func handleMsgMhfStateCampaign(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfStateCampaign)
	bf := byteframe.NewByteFrame()
	var required int
	var deadline time.Time
	var stamps []uint32

	err := s.server.db.Select(&stamps, "SELECT id FROM campaign_state WHERE campaign_id = $1 AND character_id = $2", pkt.CampaignID, s.charID)
	if err != nil {
		doAckBufFail(s, pkt.AckHandle, make([]byte, 4))
		return
	}
	err = s.server.db.QueryRow(`SELECT stamps, end_time FROM campaigns WHERE id = $1`, pkt.CampaignID).Scan(&required, &deadline)
	if err != nil {
		doAckBufFail(s, pkt.AckHandle, make([]byte, 4))
		return
	}
	bf.WriteUint16(uint16(len(stamps) + 1))

	if required == 0 {
		required = 1 // TODO: I don't understand how this is supposed to work
	}

	if len(stamps) < required {
		bf.WriteUint16(0)
	} else if len(stamps) >= required || deadline.After(time.Now()) {
		bf.WriteUint16(2)
	}

	for _, v := range stamps {
		bf.WriteUint32(v)
	}

	doAckBufSucceed(s, pkt.AckHandle, bf.Data())
}

func handleMsgMhfApplyCampaign(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfApplyCampaign)

	// Check if the code exists and check if it's a multi-code
	var multi bool
	err := s.server.db.QueryRow(`SELECT multi FROM public.campaign_codes WHERE code = $1 GROUP BY multi`, pkt.Code).Scan(&multi)
	if err != nil {
		doAckSimpleFail(s, pkt.AckHandle, make([]byte, 4))
		return
	}

	// Check if the code is already used
	var exists bool
	if multi {
		s.server.db.QueryRow(`SELECT COUNT(*) FROM public.campaign_state WHERE code = $1 AND character_id = $2`, pkt.Code, s.charID).Scan(&exists)
	} else {
		s.server.db.QueryRow(`SELECT COUNT(*) FROM public.campaign_state WHERE code = $1`, pkt.Code).Scan(&exists)
	}
	if exists {
		doAckSimpleFail(s, pkt.AckHandle, make([]byte, 4))
		return
	}

	s.server.db.Exec(`INSERT INTO public.campaign_state (code, campaign_id, character_id) VALUES ($1, $2, $3)`, pkt.Code, pkt.CampaignID, s.charID)
	doAckSimpleSucceed(s, pkt.AckHandle, make([]byte, 4))
}

func handleMsgMhfEnumerateItem(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfEnumerateItem)
	bf := byteframe.NewByteFrame()

	var stamps, required, rewardType uint16
	var deadline time.Time
	err := s.server.db.QueryRow(`SELECT COUNT(*) FROM campaign_state WHERE campaign_id = $1 AND character_id = $2`, pkt.CampaignID, s.charID).Scan(&stamps)
	if err != nil {
		doAckBufSucceed(s, pkt.AckHandle, make([]byte, 4))
		return
	}
	err = s.server.db.QueryRow(`SELECT stamps, reward_type, end_time FROM campaigns WHERE id = $1`, pkt.CampaignID).Scan(&required, &rewardType, &deadline)
	if err != nil {
		doAckBufSucceed(s, pkt.AckHandle, make([]byte, 4))
		return
	}

	if required == 0 {
		required = 1 // TODO: I don't understand how this is supposed to work
	}

	if stamps >= required {
		var items []CampaignReward
		if rewardType == 2 {
			var exists int
			s.server.db.QueryRow(`SELECT COUNT(*) FROM campaign_quest WHERE campaign_id = $1 AND character_id = $2`, pkt.CampaignID, s.charID).Scan(&exists)
			if exists > 0 {
				err = s.server.db.Select(&items, `
					SELECT id, item_type, quantity, item_id, TO_TIMESTAMP(0) AS deadline FROM campaign_rewards
					WHERE campaign_id = $1 AND item_type != 9
					AND NOT EXISTS (SELECT 1 FROM campaign_rewards_claimed WHERE reward_id = campaign_rewards.id AND character_id = $2)
				`, pkt.CampaignID, s.charID)
			} else {
				err = s.server.db.Select(&items, `
					SELECT cr.id, cr.item_type, cr.quantity, cr.item_id, COALESCE(c.end_time, TO_TIMESTAMP(0)) AS deadline FROM campaign_rewards cr
					JOIN campaigns c ON cr.campaign_id = c.id
                    WHERE campaign_id = $1 AND item_type = 9`, pkt.CampaignID)
			}
		} else {
			err = s.server.db.Select(&items, `
				SELECT id, item_type, quantity, item_id, TO_TIMESTAMP(0) AS deadline FROM campaign_rewards
				WHERE campaign_id = $1
				AND NOT EXISTS (SELECT 1 FROM campaign_rewards_claimed WHERE reward_id = campaign_rewards.id AND character_id = $2)
			`, pkt.CampaignID, s.charID)
		}
		if err != nil {
			doAckBufSucceed(s, pkt.AckHandle, make([]byte, 4))
			return
		}

		bf.WriteUint16(uint16(len(items)))
		for _, item := range items {
			bf.WriteUint32(item.ID)
			bf.WriteUint16(item.ItemType)
			bf.WriteUint16(item.Quantity)
			bf.WriteUint16(item.ItemID) //HACK:placed quest id in this field to fit with Item No pattern. however it could be another field... possibly the other unks.
			bf.WriteUint16(0)           //Unk4, gets cast to uint8
			bf.WriteUint32(0)           //Unk5
			bf.WriteUint32(uint32(deadline.Unix()))
		}
		if len(items) == 0 {
			doAckBufSucceed(s, pkt.AckHandle, make([]byte, 4))
		} else {
			doAckBufSucceed(s, pkt.AckHandle, bf.Data())
		}
	} else {
		doAckBufSucceed(s, pkt.AckHandle, make([]byte, 4))
	}
}

func handleMsgMhfAcquireItem(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfAcquireItem)
	for _, id := range pkt.RewardIDs {
		s.server.db.Exec(`INSERT INTO campaign_rewards_claimed (reward_id, character_id) VALUES ($1, $2)`, id, s.charID)
	}
	doAckSimpleSucceed(s, pkt.AckHandle, make([]byte, 4))
}

func handleMsgMhfTransferItem(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfTransferItem)
	if pkt.ItemType == 9 {
		var campaignID uint32
		err := s.server.db.QueryRow(`
			SELECT ce.campaign_id FROM campaign_rewards ce
			JOIN event_quests eq ON ce.item_id = eq.quest_id
			WHERE eq.id = $1
		`, pkt.QuestID).Scan(&campaignID)
		if err == nil {
			s.server.db.Exec(`INSERT INTO campaign_quest (campaign_id, character_id) VALUES ($1, $2)`, campaignID, s.charID)
		}
	}
	doAckSimpleSucceed(s, pkt.AckHandle, make([]byte, 4))
}
