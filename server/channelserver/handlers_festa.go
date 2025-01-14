package channelserver

import (
	"erupe-ce/common/byteframe"
	ps "erupe-ce/common/pascalstring"
	"erupe-ce/common/token"
	_config "erupe-ce/config"
	"erupe-ce/network/mhfpacket"
	"sort"
	"time"
)

func handleMsgMhfSaveMezfesData(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfSaveMezfesData)
	s.server.db.Exec(`UPDATE characters SET mezfes=$1 WHERE id=$2`, pkt.RawDataPayload, s.charID)
	doAckSimpleSucceed(s, pkt.AckHandle, []byte{0x00, 0x00, 0x00, 0x00})
}

func handleMsgMhfLoadMezfesData(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfLoadMezfesData)
	var data []byte
	s.server.db.QueryRow(`SELECT mezfes FROM characters WHERE id=$1`, s.charID).Scan(&data)
	bf := byteframe.NewByteFrame()
	if len(data) > 0 {
		bf.WriteBytes(data)
	} else {
		bf.WriteUint32(0)
		bf.WriteUint8(2)
		bf.WriteUint32(0)
		bf.WriteUint32(0)
		bf.WriteUint32(0)
	}
	doAckBufSucceed(s, pkt.AckHandle, bf.Data())
}

func handleMsgMhfEnumerateRanking(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfEnumerateRanking)
	bf := byteframe.NewByteFrame()
	state := s.server.erupeConfig.DebugOptions.TournamentOverride
	// Unk
	// Unk
	// Start?
	// End?
	midnight := TimeMidnight()
	switch state {
	case 1:
		bf.WriteUint32(uint32(midnight.Unix()))
		bf.WriteUint32(uint32(midnight.Add(3 * 24 * time.Hour).Unix()))
		bf.WriteUint32(uint32(midnight.Add(13 * 24 * time.Hour).Unix()))
		bf.WriteUint32(uint32(midnight.Add(20 * 24 * time.Hour).Unix()))
	case 2:
		bf.WriteUint32(uint32(midnight.Add(-3 * 24 * time.Hour).Unix()))
		bf.WriteUint32(uint32(midnight.Unix()))
		bf.WriteUint32(uint32(midnight.Add(10 * 24 * time.Hour).Unix()))
		bf.WriteUint32(uint32(midnight.Add(17 * 24 * time.Hour).Unix()))
	case 3:
		bf.WriteUint32(uint32(midnight.Add(-13 * 24 * time.Hour).Unix()))
		bf.WriteUint32(uint32(midnight.Add(-10 * 24 * time.Hour).Unix()))
		bf.WriteUint32(uint32(midnight.Unix()))
		bf.WriteUint32(uint32(midnight.Add(7 * 24 * time.Hour).Unix()))
	default:
		bf.WriteBytes(make([]byte, 16))
		bf.WriteUint32(uint32(TimeAdjusted().Unix())) // TS Current Time
		bf.WriteUint8(3)
		bf.WriteBytes(make([]byte, 4))
		doAckBufSucceed(s, pkt.AckHandle, bf.Data())
		return
	}
	bf.WriteUint32(uint32(TimeAdjusted().Unix())) // TS Current Time
	bf.WriteUint8(3)
	ps.Uint8(bf, "", false)
	bf.WriteUint16(0) // numEvents
	bf.WriteUint8(0)  // numCups

	/*
		struct event
		uint32 eventID
		uint16 unk
		uint16 unk
		uint32 unk
		psUint8 name

		struct cup
		uint32 cupID
		uint16 unk
		uint16 unk
		uint16 unk
		psUint8 name
		psUint16 desc
	*/

	doAckBufSucceed(s, pkt.AckHandle, bf.Data())
}

func cleanupFesta(s *Session) {
	s.server.db.Exec("DELETE FROM events WHERE event_type='festa'")
	s.server.db.Exec("DELETE FROM festa_registrations")
	s.server.db.Exec("DELETE FROM festa_submissions")
	s.server.db.Exec("DELETE FROM festa_prizes_accepted")
	s.server.db.Exec("UPDATE guild_characters SET trial_vote=NULL")
}

func generateFestaTimestamps(s *Session, start uint32, debug bool) []uint32 {
	timestamps := make([]uint32, 5)
	midnight := TimeMidnight()
	if debug && start <= 3 {
		midnight := uint32(midnight.Unix())
		switch start {
		case 1:
			timestamps[0] = midnight
			timestamps[1] = timestamps[0] + 604800
			timestamps[2] = timestamps[1] + 604800
			timestamps[3] = timestamps[2] + 9000
			timestamps[4] = timestamps[3] + 1240200
		case 2:
			timestamps[0] = midnight - 604800
			timestamps[1] = midnight
			timestamps[2] = timestamps[1] + 604800
			timestamps[3] = timestamps[2] + 9000
			timestamps[4] = timestamps[3] + 1240200
		case 3:
			timestamps[0] = midnight - 1209600
			timestamps[1] = midnight - 604800
			timestamps[2] = midnight
			timestamps[3] = timestamps[2] + 9000
			timestamps[4] = timestamps[3] + 1240200
		}
		return timestamps
	}
	if start == 0 || TimeAdjusted().Unix() > int64(start)+2977200 {
		cleanupFesta(s)
		// Generate a new festa, starting midnight tomorrow
		start = uint32(midnight.Add(24 * time.Hour).Unix())
		s.server.db.Exec("INSERT INTO events (event_type, start_time) VALUES ('festa', to_timestamp($1)::timestamp without time zone)", start)
	}
	timestamps[0] = start
	timestamps[1] = timestamps[0] + 604800
	timestamps[2] = timestamps[1] + 604800
	timestamps[3] = timestamps[2] + 9000
	timestamps[4] = timestamps[3] + 1240200
	return timestamps
}

type FestaTrial struct {
	ID        uint32        `db:"id"`
	Objective uint16        `db:"objective"`
	GoalID    uint32        `db:"goal_id"`
	TimesReq  uint16        `db:"times_req"`
	Locale    uint16        `db:"locale_req"`
	Reward    uint16        `db:"reward"`
	Monopoly  FestivalColor `db:"monopoly"`
	Unk       uint16
}

type FestaReward struct {
	Unk0     uint8
	Unk1     uint8
	ItemType uint16
	Quantity uint16
	ItemID   uint16
	Unk5     uint16
	Unk6     uint16
	Unk7     uint8
}

func handleMsgMhfInfoFesta(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfInfoFesta)
	bf := byteframe.NewByteFrame()

	id, start := uint32(0xDEADBEEF), uint32(0)
	rows, _ := s.server.db.Queryx("SELECT id, (EXTRACT(epoch FROM start_time)::int) as start_time FROM events WHERE event_type='festa'")
	for rows.Next() {
		rows.Scan(&id, &start)
	}

	var timestamps []uint32
	if s.server.erupeConfig.DebugOptions.FestaOverride >= 0 {
		if s.server.erupeConfig.DebugOptions.FestaOverride == 0 {
			doAckBufSucceed(s, pkt.AckHandle, make([]byte, 4))
			return
		}
		timestamps = generateFestaTimestamps(s, uint32(s.server.erupeConfig.DebugOptions.FestaOverride), true)
	} else {
		timestamps = generateFestaTimestamps(s, start, false)
	}

	if timestamps[0] > uint32(TimeAdjusted().Unix()) {
		doAckBufSucceed(s, pkt.AckHandle, make([]byte, 4))
		return
	}

	var blueSouls, redSouls uint32
	s.server.db.QueryRow(`SELECT COALESCE(SUM(fs.souls), 0) AS souls FROM festa_registrations fr LEFT JOIN festa_submissions fs ON fr.guild_id = fs.guild_id AND fr.team = 'blue'`).Scan(&blueSouls)
	s.server.db.QueryRow(`SELECT COALESCE(SUM(fs.souls), 0) AS souls FROM festa_registrations fr LEFT JOIN festa_submissions fs ON fr.guild_id = fs.guild_id AND fr.team = 'red'`).Scan(&redSouls)

	bf.WriteUint32(id)
	for _, timestamp := range timestamps {
		bf.WriteUint32(timestamp)
	}
	bf.WriteUint32(uint32(TimeAdjusted().Unix()))
	bf.WriteUint8(4)
	ps.Uint8(bf, "", false)
	bf.WriteUint32(0)
	bf.WriteUint32(blueSouls)
	bf.WriteUint32(redSouls)

	var trials []FestaTrial
	var trial FestaTrial
	rows, _ = s.server.db.Queryx(`SELECT ft.*,
		COALESCE(CASE
			WHEN COUNT(gc.id) FILTER (WHERE fr.team = 'blue' AND gc.trial_vote = ft.id) >
				 COUNT(gc.id) FILTER (WHERE fr.team = 'red' AND gc.trial_vote = ft.id)
			THEN CAST('blue' AS public.festival_color)
			WHEN COUNT(gc.id) FILTER (WHERE fr.team = 'red' AND gc.trial_vote = ft.id) >
				 COUNT(gc.id) FILTER (WHERE fr.team = 'blue' AND gc.trial_vote = ft.id)
			THEN CAST('red' AS public.festival_color)
		END, CAST('none' AS public.festival_color)) AS monopoly
		FROM public.festa_trials ft
		LEFT JOIN public.guild_characters gc ON ft.id = gc.trial_vote
		LEFT JOIN public.festa_registrations fr ON gc.guild_id = fr.guild_id
		GROUP BY ft.id`)
	for rows.Next() {
		err := rows.StructScan(&trial)
		if err != nil {
			continue
		}
		
		switch _config.ErupeConfig.RealClientMode {
		case _config.F5:
			if (trial.Objective == 1 || trial.Objective == 2 || trial.Objective == 3) && trial.GoalID > 106 {
				continue
			}
			if trial.Objective == 4 && trial.GoalID > 6430 {
				continue
			}
		}
		trials = append(trials, trial)
	}
	bf.WriteUint16(uint16(len(trials)))
	for _, trial := range trials {
		bf.WriteUint32(trial.ID)
		bf.WriteUint16(trial.Objective)
		bf.WriteUint32(trial.GoalID)
		bf.WriteUint16(trial.TimesReq)
		bf.WriteUint16(trial.Locale)
		bf.WriteUint16(trial.Reward)
		bf.WriteInt16(FestivalColorCodes[trial.Monopoly])
		if _config.ErupeConfig.RealClientMode >= _config.F4 { // Not in S6.0
			bf.WriteUint16(trial.Unk)
		}
	}

	// The Winner and Loser Armor IDs are missing
	rewards := []FestaReward{
		{1, 0, 7, 350, 1520, 0, 0, 0},
		{1, 0, 7, 1000, 7011, 0, 0, 1},
		{1, 0, 12, 1000, 0, 0, 0, 0},
		{1, 0, 13, 0, 0, 0, 0, 0},
		//{1, 0, 1, 0, 0, 0, 0, 0},
		{2, 0, 7, 350, 1520, 0, 0, 0},
		{2, 0, 7, 1000, 7011, 0, 0, 1},
		{2, 0, 12, 1000, 0, 0, 0, 0},
		{2, 0, 13, 0, 0, 0, 0, 0},
		//{2, 0, 4, 0, 0, 0, 0, 0},
		{3, 0, 7, 350, 1520, 0, 0, 0},
		{3, 0, 7, 1000, 7011, 0, 0, 1},
		{3, 0, 12, 1000, 0, 0, 0, 0},
		{3, 0, 13, 0, 0, 0, 0, 0},
		//{3, 0, 1, 0, 0, 0, 0, 0},
		{4, 0, 7, 350, 1520, 0, 0, 0},
		{4, 0, 7, 1000, 7011, 0, 0, 1},
		{4, 0, 12, 1000, 0, 0, 0, 0},
		{4, 0, 13, 0, 0, 0, 0, 0},
		//{4, 0, 4, 0, 0, 0, 0, 0},
		{5, 0, 7, 350, 1520, 0, 0, 0},
		{5, 0, 7, 1000, 7011, 0, 0, 1},
		{5, 0, 12, 1000, 0, 0, 0, 0},
		{5, 0, 13, 0, 0, 0, 0, 0},
		//{5, 0, 1, 0, 0, 0, 0, 0},
	}

	// Item 7011 does not exist in older versions, filtering to prevent crashes
	filteredRewards := []FestaReward{}
	for _, retroReward := range rewards {
		if _config.ErupeConfig.RealClientMode <= _config.F5 && retroReward.ItemType == 7 && retroReward.ItemID == 7011 {
			continue
		}
		filteredRewards = append(filteredRewards, retroReward)
	}

	bf.WriteUint16(uint16(len(rewards)))
	for _, reward := range rewards {
		bf.WriteUint8(reward.Unk0)
		bf.WriteUint8(reward.Unk1)
		bf.WriteUint16(reward.ItemType)
		bf.WriteUint16(reward.Quantity)
		bf.WriteUint16(reward.ItemID)
		// Not confirmed to be G1 but exists in G3
		if _config.ErupeConfig.RealClientMode >= _config.G1 {
			bf.WriteUint16(reward.Unk5)
			bf.WriteUint16(reward.Unk6)
			bf.WriteUint8(reward.Unk7)
		}
	}
	if _config.ErupeConfig.RealClientMode <= _config.G61 {
		if s.server.erupeConfig.GameplayOptions.MaximumFP > 0xFFFF {
			s.server.erupeConfig.GameplayOptions.MaximumFP = 0xFFFF
		}
		bf.WriteUint16(uint16(s.server.erupeConfig.GameplayOptions.MaximumFP))
	} else {
		bf.WriteUint32(s.server.erupeConfig.GameplayOptions.MaximumFP)
	}
	bf.WriteUint16(100) // Reward multiplier (%)

	var temp uint32
	bf.WriteUint16(4)
	for i := uint16(0); i < 4; i++ {
		var guildID uint32
		var guildName string
		var guildTeam = FestivalColorNone
		s.server.db.QueryRow(`
				SELECT fs.guild_id, g.name, fr.team, SUM(fs.souls) as _
				FROM festa_submissions fs
				LEFT JOIN festa_registrations fr ON fs.guild_id = fr.guild_id
				LEFT JOIN guilds g ON fs.guild_id = g.id
				WHERE fs.trial_type = $1
				GROUP BY fs.guild_id, g.name, fr.team
				ORDER BY _ DESC LIMIT 1
			`, i+1).Scan(&guildID, &guildName, &guildTeam, &temp)
		bf.WriteUint32(guildID)
		bf.WriteUint16(i + 1)
		bf.WriteInt16(FestivalColorCodes[guildTeam])
		ps.Uint8(bf, guildName, true)
	}
	bf.WriteUint16(7)
	for i := uint16(0); i < 7; i++ {
		var guildID uint32
		var guildName string
		var guildTeam = FestivalColorNone
		offset := 86400 * uint32(i)
		s.server.db.QueryRow(`
				SELECT fs.guild_id, g.name, fr.team, SUM(fs.souls) as _
				FROM festa_submissions fs
				LEFT JOIN festa_registrations fr ON fs.guild_id = fr.guild_id
				LEFT JOIN guilds g ON fs.guild_id = g.id
				WHERE EXTRACT(EPOCH FROM fs.timestamp)::int > $1 AND EXTRACT(EPOCH FROM fs.timestamp)::int < $2
				GROUP BY fs.guild_id, g.name, fr.team
				ORDER BY _ DESC LIMIT 1
			`, timestamps[1]+offset, timestamps[1]+offset+86400).Scan(&guildID, &guildName, &guildTeam, &temp)
		bf.WriteUint32(guildID)
		bf.WriteUint16(i + 1)
		bf.WriteInt16(FestivalColorCodes[guildTeam])
		ps.Uint8(bf, guildName, true)
	}

	bf.WriteUint32(0) // Clan goal
	// Final bonus rates
	bf.WriteUint32(5000) // 5000+ souls
	bf.WriteUint32(2000) // 2000-4999 souls
	bf.WriteUint32(1000) // 1000-1999 souls
	bf.WriteUint32(100)  // 100-999 souls
	bf.WriteUint16(300)  // 300% bonus
	bf.WriteUint16(200)  // 200% bonus
	bf.WriteUint16(150)  // 150% bonus
	bf.WriteUint16(100)  // Normal rate
	bf.WriteUint16(50)   // 50% penalty

	if _config.ErupeConfig.RealClientMode >= _config.G52 {
		ps.Uint16(bf, "", false)
	}
	doAckBufSucceed(s, pkt.AckHandle, bf.Data())
}

// state festa (U)ser
func handleMsgMhfStateFestaU(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfStateFestaU)
	guild, err := GetGuildInfoByCharacterId(s, s.charID)
	applicant := false
	if guild != nil {
		applicant, _ = guild.HasApplicationForCharID(s, s.charID)
	}
	if err != nil || guild == nil || applicant {
		doAckSimpleFail(s, pkt.AckHandle, make([]byte, 4))
		return
	}
	var souls, exists uint32
	s.server.db.QueryRow(`SELECT COALESCE((SELECT SUM(souls) FROM festa_submissions WHERE character_id=$1), 0)`, s.charID).Scan(&souls)
	err = s.server.db.QueryRow("SELECT prize_id FROM festa_prizes_accepted WHERE prize_id=0 AND character_id=$1", s.charID).Scan(&exists)
	bf := byteframe.NewByteFrame()
	bf.WriteUint32(souls)
	if err != nil {
		bf.WriteBool(true)
		bf.WriteBool(false)
	} else {
		bf.WriteBool(false)
		bf.WriteBool(true)
	}
	doAckBufSucceed(s, pkt.AckHandle, bf.Data())
}

// state festa (G)uild
func handleMsgMhfStateFestaG(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfStateFestaG)
	guild, err := GetGuildInfoByCharacterId(s, s.charID)
	applicant := false
	if guild != nil {
		applicant, _ = guild.HasApplicationForCharID(s, s.charID)
	}
	resp := byteframe.NewByteFrame()
	if err != nil || guild == nil || applicant {
		resp.WriteUint32(0)
		resp.WriteInt32(0)
		resp.WriteInt32(-1)
		resp.WriteInt32(0)
		resp.WriteInt32(0)
		doAckBufSucceed(s, pkt.AckHandle, resp.Data())
		return
	}
	resp.WriteUint32(guild.Souls)
	resp.WriteInt32(1) // unk
	resp.WriteInt32(1) // unk, rank?
	resp.WriteInt32(1) // unk
	resp.WriteInt32(1) // unk
	doAckBufSucceed(s, pkt.AckHandle, resp.Data())
}

func handleMsgMhfEnumerateFestaMember(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfEnumerateFestaMember)
	guild, err := GetGuildInfoByCharacterId(s, s.charID)
	if err != nil || guild == nil {
		doAckSimpleFail(s, pkt.AckHandle, make([]byte, 4))
		return
	}
	members, err := GetGuildMembers(s, guild.ID, false)
	if err != nil {
		doAckSimpleFail(s, pkt.AckHandle, make([]byte, 4))
		return
	}
	sort.Slice(members, func(i, j int) bool {
		return members[i].Souls > members[j].Souls
	})
	var validMembers []*GuildMember
	for _, member := range members {
		if member.Souls > 0 {
			validMembers = append(validMembers, member)
		}
	}
	bf := byteframe.NewByteFrame()
	bf.WriteUint16(uint16(len(validMembers)))
	bf.WriteUint16(0) // Unk
	for _, member := range validMembers {
		bf.WriteUint32(member.CharID)
		if _config.ErupeConfig.RealClientMode <= _config.Z1 {
			bf.WriteUint16(uint16(member.Souls))
			bf.WriteUint16(0)
		} else {
			bf.WriteUint32(member.Souls)
		}
	}
	doAckBufSucceed(s, pkt.AckHandle, bf.Data())
}

func handleMsgMhfVoteFesta(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfVoteFesta)
	s.server.db.Exec(`UPDATE guild_characters SET trial_vote=$1 WHERE character_id=$2`, pkt.TrialID, s.charID)
	doAckSimpleSucceed(s, pkt.AckHandle, make([]byte, 4))
}

func handleMsgMhfEntryFesta(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfEntryFesta)
	guild, err := GetGuildInfoByCharacterId(s, s.charID)
	if err != nil || guild == nil {
		doAckSimpleFail(s, pkt.AckHandle, make([]byte, 4))
		return
	}
	team := uint32(token.RNG.Intn(2))
	switch team {
	case 0:
		s.server.db.Exec("INSERT INTO festa_registrations VALUES ($1, 'blue')", guild.ID)
	case 1:
		s.server.db.Exec("INSERT INTO festa_registrations VALUES ($1, 'red')", guild.ID)
	}
	bf := byteframe.NewByteFrame()
	bf.WriteUint32(team)
	doAckSimpleSucceed(s, pkt.AckHandle, bf.Data())
}

func handleMsgMhfChargeFesta(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfChargeFesta)
	tx, _ := s.server.db.Begin()
	for i := range pkt.Souls {
		if pkt.Souls[i] == 0 {
			continue
		}
		_, _ = tx.Exec(`INSERT INTO festa_submissions VALUES ($1, $2, $3, $4, now())`, s.charID, pkt.GuildID, i, pkt.Souls[i])
	}
	_ = tx.Commit()
	doAckSimpleSucceed(s, pkt.AckHandle, make([]byte, 4))
}

func handleMsgMhfAcquireFesta(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfAcquireFesta)
	s.server.db.Exec("INSERT INTO public.festa_prizes_accepted VALUES (0, $1)", s.charID)
	doAckSimpleSucceed(s, pkt.AckHandle, make([]byte, 4))
}

func handleMsgMhfAcquireFestaPersonalPrize(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfAcquireFestaPersonalPrize)
	s.server.db.Exec("INSERT INTO public.festa_prizes_accepted VALUES ($1, $2)", pkt.PrizeID, s.charID)
	doAckSimpleSucceed(s, pkt.AckHandle, make([]byte, 4))
}

func handleMsgMhfAcquireFestaIntermediatePrize(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfAcquireFestaIntermediatePrize)
	s.server.db.Exec("INSERT INTO public.festa_prizes_accepted VALUES ($1, $2)", pkt.PrizeID, s.charID)
	doAckSimpleSucceed(s, pkt.AckHandle, make([]byte, 4))
}

type Prize struct {
	ID       uint32 `db:"id"`
	Tier     uint32 `db:"tier"`
	SoulsReq uint32 `db:"souls_req"`
	ItemID   uint32 `db:"item_id"`
	NumItem  uint32 `db:"num_item"`
	Claimed  int    `db:"claimed"`
}

func handleMsgMhfEnumerateFestaPersonalPrize(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfEnumerateFestaPersonalPrize)
	rows, _ := s.server.db.Queryx(`SELECT id, tier, souls_req, item_id, num_item, (SELECT count(*) FROM festa_prizes_accepted fpa WHERE fp.id = fpa.prize_id AND fpa.character_id = $1) AS claimed FROM festa_prizes fp WHERE type='personal'`, s.charID)
	var count uint32
	prizeData := byteframe.NewByteFrame()
	for rows.Next() {
		prize := &Prize{}
		err := rows.StructScan(&prize)
		if err != nil {
			continue
		}
		count++
		prizeData.WriteUint32(prize.ID)
		prizeData.WriteUint32(prize.Tier)
		prizeData.WriteUint32(prize.SoulsReq)
		prizeData.WriteUint32(7) // Unk
		prizeData.WriteUint32(prize.ItemID)
		prizeData.WriteUint32(prize.NumItem)
		prizeData.WriteBool(prize.Claimed > 0)
	}
	bf := byteframe.NewByteFrame()
	bf.WriteUint32(count)
	bf.WriteBytes(prizeData.Data())
	doAckBufSucceed(s, pkt.AckHandle, bf.Data())
}

func handleMsgMhfEnumerateFestaIntermediatePrize(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfEnumerateFestaIntermediatePrize)
	rows, _ := s.server.db.Queryx(`SELECT id, tier, souls_req, item_id, num_item, (SELECT count(*) FROM festa_prizes_accepted fpa WHERE fp.id = fpa.prize_id AND fpa.character_id = $1) AS claimed FROM festa_prizes fp WHERE type='guild'`, s.charID)
	var count uint32
	prizeData := byteframe.NewByteFrame()
	for rows.Next() {
		prize := &Prize{}
		err := rows.StructScan(&prize)
		if err != nil {
			continue
		}
		count++
		prizeData.WriteUint32(prize.ID)
		prizeData.WriteUint32(prize.Tier)
		prizeData.WriteUint32(prize.SoulsReq)
		prizeData.WriteUint32(7) // Unk
		prizeData.WriteUint32(prize.ItemID)
		prizeData.WriteUint32(prize.NumItem)
		prizeData.WriteBool(prize.Claimed > 0)
	}
	bf := byteframe.NewByteFrame()
	bf.WriteUint32(count)
	bf.WriteBytes(prizeData.Data())
	doAckBufSucceed(s, pkt.AckHandle, bf.Data())
}
