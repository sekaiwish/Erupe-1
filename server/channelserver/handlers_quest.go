package channelserver

import (
	"database/sql"
	"encoding/binary"
	"erupe-ce/common/byteframe"
	"erupe-ce/common/decryption"
	ps "erupe-ce/common/pascalstring"
	_config "erupe-ce/config"
	"erupe-ce/network/mhfpacket"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"
)

type tuneValue struct {
	ID    uint16
	Value uint16
}

func findSubSliceIndices(data []byte, sub []byte) []int {
	var indices []int
	lenSub := len(sub)
	for i := 0; i < len(data); i++ {
		if i+lenSub > len(data) {
			break
		}
		if equal(data[i:i+lenSub], sub) {
			indices = append(indices, i)
		}
	}
	return indices
}

func equal(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

func BackportQuest(data []byte) []byte {
	wp := binary.LittleEndian.Uint32(data[0:4]) + 96
	rp := wp + 4
	for i := uint32(0); i < 6; i++ {
		if i != 0 {
			wp += 4
			rp += 8
		}
		copy(data[wp:wp+4], data[rp:rp+4])
	}

	fillLength := uint32(108)
	if _config.ErupeConfig.RealClientMode <= _config.S6 {
		fillLength = 44
	} else if _config.ErupeConfig.RealClientMode <= _config.F5 {
		fillLength = 52
	} else if _config.ErupeConfig.RealClientMode <= _config.G101 {
		fillLength = 76
	}

	copy(data[wp:wp+fillLength], data[rp:rp+fillLength])
	if _config.ErupeConfig.RealClientMode <= _config.G91 {
		patterns := [][]byte{
			{0x0A, 0x00, 0x01, 0x33, 0xD7, 0x00}, // 10% Armor Sphere -> Stone
			{0x06, 0x00, 0x02, 0x33, 0xD8, 0x00}, // 6% Armor Sphere+ -> Iron Ore
			{0x0A, 0x00, 0x03, 0x33, 0xD7, 0x00}, // 10% Adv Armor Sphere -> Stone
			{0x06, 0x00, 0x04, 0x33, 0xDB, 0x00}, // 6% Hard Armor Sphere -> Dragonite Ore
			{0x0A, 0x00, 0x05, 0x33, 0xD9, 0x00}, // 10% Heaven Armor Sphere -> Earth Crystal
			{0x06, 0x00, 0x06, 0x33, 0xDB, 0x00}, // 6% True Armor Sphere -> Dragonite Ore
		}
		for i := range patterns {
			j := findSubSliceIndices(data, patterns[i][0:4])
			for k := range j {
				copy(data[j[k]+2:j[k]+4], patterns[i][4:6])
			}
		}
	}
	return data
}

func handleMsgSysGetFile(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgSysGetFile)

	if pkt.IsScenario {
		if s.server.erupeConfig.DebugOptions.QuestTools {
			s.logger.Debug(
				"Scenario",
				zap.Uint8("CategoryID", pkt.ScenarioIdentifer.CategoryID),
				zap.Uint32("MainID", pkt.ScenarioIdentifer.MainID),
				zap.Uint8("ChapterID", pkt.ScenarioIdentifer.ChapterID),
				zap.Uint8("Flags", pkt.ScenarioIdentifer.Flags),
			)
		}
		filename := fmt.Sprintf("%d_0_0_0_S%d_T%d_C%d", pkt.ScenarioIdentifer.CategoryID, pkt.ScenarioIdentifer.MainID, pkt.ScenarioIdentifer.Flags, pkt.ScenarioIdentifer.ChapterID)
		// Read the scenario file.
		data, err := os.ReadFile(filepath.Join(s.server.erupeConfig.BinPath, fmt.Sprintf("scenarios/%s.bin", filename)))
		if err != nil {
			s.logger.Error(fmt.Sprintf("Failed to open file: %s/scenarios/%s.bin", s.server.erupeConfig.BinPath, filename))
			// This will crash the game.
			doAckBufSucceed(s, pkt.AckHandle, data)
			return
		}
		doAckBufSucceed(s, pkt.AckHandle, data)
	} else {
		if s.server.erupeConfig.DebugOptions.QuestTools {
			s.logger.Debug(
				"Quest",
				zap.String("Filename", pkt.Filename),
			)
		}

		if s.server.erupeConfig.GameplayOptions.SeasonOverride {
			pkt.Filename = seasonConversion(s, pkt.Filename)
		}

		data, err := os.ReadFile(filepath.Join(s.server.erupeConfig.BinPath, fmt.Sprintf("quests/%s.bin", pkt.Filename)))
		if err != nil {
			s.logger.Error(fmt.Sprintf("Failed to open file: %s/quests/%s.bin", s.server.erupeConfig.BinPath, pkt.Filename))
			// This will crash the game.
			doAckBufSucceed(s, pkt.AckHandle, data)
			return
		}
		if _config.ErupeConfig.RealClientMode <= _config.Z1 && s.server.erupeConfig.DebugOptions.AutoQuestBackport {
			data = BackportQuest(decryption.UnpackSimple(data))
		}
		doAckBufSucceed(s, pkt.AckHandle, data)
	}
}

func seasonConversion(s *Session, questFile string) string {
	filename := fmt.Sprintf("%s%d", questFile[:6], s.server.Season())

	// Return the seasonal file
	if _, err := os.Stat(filepath.Join(s.server.erupeConfig.BinPath, fmt.Sprintf("quests/%s.bin", filename))); err == nil {
		return filename
	} else {
		// Attempt to return the requested quest file if the seasonal file doesn't exist
		if _, err = os.Stat(filepath.Join(s.server.erupeConfig.BinPath, fmt.Sprintf("quests/%s.bin", questFile))); err == nil {
			return questFile
		}

		// If the code reaches this point, it's most likely a custom quest with no seasonal variations in the files.
		// Since event quests when seasonal pick day or night and the client requests either one, we need to differentiate between the two to prevent issues.
		var _time string

		if TimeGameAbsolute() > 2880 {
			_time = "d"
		} else {
			_time = "n"
		}

		// Request a d0 or n0 file depending on the time of day. The time of day matters and issues will occur if it's different to the one it requests.
		return fmt.Sprintf("%s%s%d", questFile[:5], _time, 0)
	}
}

func handleMsgMhfLoadFavoriteQuest(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfLoadFavoriteQuest)
	var data []byte
	err := s.server.db.QueryRow("SELECT savefavoritequest FROM characters WHERE id = $1", s.charID).Scan(&data)
	if err == nil && len(data) > 0 {
		doAckBufSucceed(s, pkt.AckHandle, data)
	} else {
		doAckBufSucceed(s, pkt.AckHandle, []byte{0x01, 0x00, 0x01, 0x00, 0x01, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	}
}

func handleMsgMhfSaveFavoriteQuest(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfSaveFavoriteQuest)
	dumpSaveData(s, pkt.Data, "favquest")
	s.server.db.Exec("UPDATE characters SET savefavoritequest=$1 WHERE id=$2", pkt.Data, s.charID)
	doAckSimpleSucceed(s, pkt.AckHandle, []byte{0x00, 0x00, 0x00, 0x00})
}

func loadQuestFile(s *Session, questId int) []byte {
	data, exists := s.server.questCacheData[questId]
	if exists && s.server.questCacheTime[questId].Add(time.Duration(s.server.erupeConfig.QuestCacheExpiry)*time.Second).After(time.Now()) {
		return data
	}

	file, err := os.ReadFile(filepath.Join(s.server.erupeConfig.BinPath, fmt.Sprintf("quests/%05dd0.bin", questId)))
	if err != nil {
		return nil
	}

	decrypted := decryption.UnpackSimple(file)
	if _config.ErupeConfig.RealClientMode <= _config.Z1 && s.server.erupeConfig.DebugOptions.AutoQuestBackport {
		decrypted = BackportQuest(decrypted)
	}
	fileBytes := byteframe.NewByteFrameFromBytes(decrypted)
	fileBytes.SetLE()
	fileBytes.Seek(int64(fileBytes.ReadUint32()), 0)

	bodyLength := 320
	if _config.ErupeConfig.RealClientMode <= _config.S6 {
		bodyLength = 160
	} else if _config.ErupeConfig.RealClientMode <= _config.F5 {
		bodyLength = 168
	} else if _config.ErupeConfig.RealClientMode <= _config.G101 {
		bodyLength = 192
	} else if _config.ErupeConfig.RealClientMode <= _config.Z1 {
		bodyLength = 224
	}

	// The n bytes directly following the data pointer must go directly into the event's body, after the header and before the string pointers.
	questBody := byteframe.NewByteFrameFromBytes(fileBytes.ReadBytes(uint(bodyLength)))
	questBody.SetLE()
	// Find the master quest string pointer
	questBody.Seek(40, 0)
	fileBytes.Seek(int64(questBody.ReadUint32()), 0)
	questBody.Seek(40, 0)
	// Overwrite it
	questBody.WriteUint32(uint32(bodyLength))
	questBody.Seek(0, 2)

	// Rewrite the quest strings and their pointers
	var tempString []byte
	newStrings := byteframe.NewByteFrame()
	tempPointer := bodyLength + 32
	for i := 0; i < 8; i++ {
		questBody.WriteUint32(uint32(tempPointer))
		temp := int64(fileBytes.Index())
		fileBytes.Seek(int64(fileBytes.ReadUint32()), 0)
		tempString = fileBytes.ReadNullTerminatedBytes()
		fileBytes.Seek(temp+4, 0)
		tempPointer += len(tempString) + 1
		newStrings.WriteNullTerminatedBytes(tempString)
	}
	questBody.WriteBytes(newStrings.Data())

	s.server.questCacheData[questId] = questBody.Data()
	s.server.questCacheTime[questId] = time.Now()
	return questBody.Data()
}

func makeEventQuest(s *Session, rows *sql.Rows) ([]byte, error) {
	var id, mark uint32
	var questId, activeDuration, inactiveDuration, flags int
	var maxPlayers, questType uint8
	var startTime time.Time
	rows.Scan(&id, &maxPlayers, &questType, &questId, &mark, &flags, &startTime, &activeDuration, &inactiveDuration)

	data := loadQuestFile(s, questId)
	if data == nil {
		return nil, fmt.Errorf(fmt.Sprintf("failed to load quest file (%d)", questId))
	}

	bf := byteframe.NewByteFrame()
	bf.WriteUint32(id)
	bf.WriteUint32(0) // Unk
	bf.WriteUint8(0)  // Unk
	switch questType {
	case 16:
		bf.WriteUint8(s.server.erupeConfig.GameplayOptions.RegularRavienteMaxPlayers)
	case 22:
		bf.WriteUint8(s.server.erupeConfig.GameplayOptions.ViolentRavienteMaxPlayers)
	case 40:
		bf.WriteUint8(s.server.erupeConfig.GameplayOptions.BerserkRavienteMaxPlayers)
	case 50:
		bf.WriteUint8(s.server.erupeConfig.GameplayOptions.ExtremeRavienteMaxPlayers)
	case 51:
		bf.WriteUint8(s.server.erupeConfig.GameplayOptions.SmallBerserkRavienteMaxPlayers)
	default:
		bf.WriteUint8(maxPlayers)
	}
	bf.WriteUint8(questType)
	if questType == 9 {
		var stamps, required int
		var deadline time.Time
		err := s.server.db.QueryRow(`SELECT COUNT(*) FROM campaign_state WHERE campaign_id = (
			SELECT campaign_id
			FROM campaign_rewards
			WHERE item_type = 9
			AND item_id = $1
		) AND character_id = $2`, questId, s.charID).Scan(&stamps)
		err2 := s.server.db.QueryRow(`SELECT stamps, end_time
		FROM campaigns
		WHERE id = (
			SELECT campaign_id
			FROM campaign_rewards
			WHERE item_type = 9
			AND item_id = $1
		)`, questId).Scan(&required, &deadline)

		if required == 0 {
			required = 1 // TODO: I don't understand how this is supposed to work
		}

		// Check if there are enough stamps to activate the quest, the deadline hasn't passed, and there are no errors
		if stamps >= required && deadline.After(time.Now()) && err == nil && err2 == nil {
			bf.WriteBool(true)
		} else {
			bf.WriteBool(false)
		}
	} else {
		bf.WriteBool(true)
	}
	bf.WriteUint16(0) // Unk
	if _config.ErupeConfig.RealClientMode >= _config.G2 {
		bf.WriteUint32(mark)
	}
	bf.WriteUint16(0) // Unk
	bf.WriteUint16(uint16(len(data)))
	bf.WriteBytes(data)

	// Time Flag Replacement
	// Bitset Structure: b8 UNK, b7 Required Objective, b6 UNK, b5 Night, b4 Day, b3 Cold, b2 Warm, b1 Spring
	// if the byte is set to 0 the game choses the quest file corresponding to whatever season the game is on
	bf.Seek(25, 0)
	flagByte := bf.ReadUint8()
	bf.Seek(25, 0)
	if s.server.erupeConfig.GameplayOptions.SeasonOverride {
		bf.WriteUint8(flagByte & 0b11100000)
	} else {
		// Allow for seasons to be specified in database, otherwise use the one in the file.
		if flags < 0 {
			bf.WriteUint8(flagByte)
		} else {
			bf.WriteUint8(uint8(flags))
		}
	}

	// Bitset Structure Quest Variant 1: b8 UL Fixed, b7 UNK, b6 UNK, b5 UNK, b4 G Rank, b3 HC to UL, b2 Fix HC, b1 Hiden
	// Bitset Structure Quest Variant 2: b8 Road, b7 High Conquest, b6 Fixed Difficulty, b5 No Active Feature, b4 Timer, b3 No Cuff, b2 No Halk Pots, b1 Low Conquest
	// Bitset Structure Quest Variant 3: b8 No Sigils, b7 UNK, b6 Interception, b5 Zenith, b4 No GP Skills, b3 No Simple Mode?, b2 GSR to GR, b1 No Reward Skills

	bf.Seek(175, 0)
	questVariant3 := bf.ReadUint8()
	questVariant3 &= 0b11011111 // disable Interception flag
	bf.Seek(175, 0)
	bf.WriteUint8(questVariant3)

	bf.Seek(0, 2)
	ps.Uint8(bf, "", true) // Debug/Notes string for quest
	return bf.Data(), nil
}

func handleMsgMhfEnumerateQuest(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfEnumerateQuest)
	var totalCount, returnedCount uint16
	bf := byteframe.NewByteFrame()
	bf.WriteUint16(0)

	rows, err := s.server.db.Query("SELECT id, COALESCE(max_players, 4) AS max_players, quest_type, quest_id, COALESCE(mark, 0) AS mark, COALESCE(flags, -1), start_time, COALESCE(active_days, 0) AS active_days, COALESCE(inactive_days, 0) AS inactive_days FROM event_quests ORDER BY quest_id")
	if err == nil {
		currentTime := time.Now()
		tx, _ := s.server.db.Begin()

		for rows.Next() {
			var id, mark uint32
			var questId, flags, activeDays, inactiveDays int
			var maxPlayers, questType uint8
			var startTime time.Time

			err = rows.Scan(&id, &maxPlayers, &questType, &questId, &mark, &flags, &startTime, &activeDays, &inactiveDays)
			if err != nil {
				s.logger.Error("Failed to scan event quest row", zap.Error(err))
				continue
			}

			// Use the Event Cycling system
			if activeDays > 0 {
				cycleLength := (time.Duration(activeDays) + time.Duration(inactiveDays)) * 24 * time.Hour

				// Count the number of full cycles elapsed since the last rotation.
				extraCycles := int(currentTime.Sub(startTime) / cycleLength)

				if extraCycles > 0 {
					// Calculate the rotation time based on start time, active duration, and inactive duration.
					rotationTime := startTime.Add(time.Duration(activeDays+inactiveDays) * 24 * time.Hour * time.Duration(extraCycles))
					if currentTime.After(rotationTime) {
						// Normalize rotationTime to 12PM JST to align with the in-game events update notification.
						newRotationTime := time.Date(rotationTime.Year(), rotationTime.Month(), rotationTime.Day(), 12, 0, 0, 0, TimeAdjusted().Location())

						_, err = tx.Exec("UPDATE event_quests SET start_time = $1 WHERE id = $2", newRotationTime, id)
						if err != nil {
							tx.Rollback() // Rollback if an error occurs
							break
						}
						startTime = newRotationTime // Set the new start time so the quest can be used/removed immediately.
					}
				}

				// Check if the quest is currently active
				if currentTime.Before(startTime) || currentTime.After(startTime.Add(time.Duration(activeDays)*24*time.Hour)) {
					continue
				}
			}

			data, err := makeEventQuest(s, rows)
			if err != nil {
				s.logger.Error("Failed to make event quest", zap.Error(err))
				continue
			} else {
				if len(data) > 896 || len(data) < 352 {
					s.logger.Error("Invalid quest data length", zap.Int("len", len(data)))
					continue
				} else {
					totalCount++
					if totalCount > pkt.Offset && len(bf.Data()) < 60000 {
						returnedCount++
						bf.WriteBytes(data)
						continue
					}
				}
			}
		}

		rows.Close()
		tx.Commit()
	}

	tuneValues := []tuneValue{
		{ID: 20, Value: 1},
		{ID: 26, Value: 1},
		{ID: 27, Value: 1},
		{ID: 33, Value: 1},
		{ID: 40, Value: 1},
		{ID: 49, Value: 1},
		{ID: 53, Value: 1},
		{ID: 59, Value: 1},
		{ID: 67, Value: 1},
		{ID: 80, Value: 1},
		{ID: 94, Value: 1},
		{ID: 1001, Value: 100},   // get_hrp_rate
		{ID: 1010, Value: 300},   // get_hrp_rate_netcafe
		{ID: 1011, Value: 300},   // get_zeny_rate_netcafe
		{ID: 1012, Value: 300},   // get_hrp_rate_ncource
		{ID: 1013, Value: 300},   // get_zeny_rate_ncource
		{ID: 1014, Value: 200},   // get_hrp_rate_premium
		{ID: 1015, Value: 200},   // get_zeny_rate_premium
		{ID: 1021, Value: 400},   // get_gcp_rate_assist
		{ID: 1023, Value: 8},     // unused?
		{ID: 1024, Value: 150},   // get_hrp_rate_ptbonus
		{ID: 1025, Value: 1},     // isValid_stampcard
		{ID: 1026, Value: 999},   // get_grank_cap
		{ID: 1027, Value: 100},   // get_exchange_rate_festa
		{ID: 1028, Value: 100},   // get_exchange_rate_cafe
		{ID: 1030, Value: 8},     // get_gquest_cap
		{ID: 1031, Value: 100},   // get_exchange_rate_guild (GCP)
		{ID: 1032, Value: 0},     // isValid_partner
		{ID: 1044, Value: 200},   // get_rate_tload_time_out
		{ID: 1045, Value: 0},     // get_rate_tower_treasure_preset
		{ID: 1046, Value: 99},    // get_hunter_life_cap
		{ID: 1048, Value: 0},     // get_rate_tower_hint_sec
		{ID: 1049, Value: 10},    // get_rate_tower_gem_max
		{ID: 1050, Value: 1},     // get_rate_tower_gem_set
		{ID: 1051, Value: 200},   // get_pallone_score_rate_premium
		{ID: 1052, Value: 200},   // get_trp_rate_premium
		{ID: 1063, Value: 50000}, // get_nboost_quest_point_from_hrank
		{ID: 1064, Value: 50000}, // get_nboost_quest_point_from_srank
		{ID: 1065, Value: 25000}, // get_nboost_quest_point_from_grank
		{ID: 1066, Value: 25000}, // get_nboost_quest_point_from_gsrank
		{ID: 1067, Value: 90},    // get_lobby_member_upper_for_making_room Lv1?
		{ID: 1068, Value: 80},    // get_lobby_member_upper_for_making_room Lv2?
		{ID: 1069, Value: 70},    // get_lobby_member_upper_for_making_room Lv3?
		{ID: 1072, Value: 300},   // get_rate_premium_ravi_tama
		{ID: 1073, Value: 300},   // get_rate_premium_ravi_ax_tama
		{ID: 1074, Value: 300},   // get_rate_premium_ravi_g_tama
		{ID: 1078, Value: 0},     // isCapped_tenrou_irai
		{ID: 1079, Value: 1},     // get_add_tower_level_assist
		{ID: 1080, Value: 1},     // get_tune_add_tower_level_w_assist_nboost

		// get_tune_secret_book_item
		{ID: 1081, Value: 1},
		{ID: 1082, Value: 4},
		{ID: 1083, Value: 2},
		{ID: 1084, Value: 10},
		{ID: 1085, Value: 1},
		{ID: 1086, Value: 4},
		{ID: 1087, Value: 2},
		{ID: 1088, Value: 10},
		{ID: 1089, Value: 1},
		{ID: 1090, Value: 3},
		{ID: 1091, Value: 2},
		{ID: 1092, Value: 10},
		{ID: 1093, Value: 2},
		{ID: 1094, Value: 5},
		{ID: 1095, Value: 2},
		{ID: 1096, Value: 10},
		{ID: 1097, Value: 2},
		{ID: 1098, Value: 5},
		{ID: 1099, Value: 2},
		{ID: 1100, Value: 10},
		{ID: 1101, Value: 2},
		{ID: 1102, Value: 5},
		{ID: 1103, Value: 2},
		{ID: 1104, Value: 10},

		{ID: 1145, Value: 200},  // get_ud_point_rate_premium
		{ID: 1146, Value: 0},    // isTower_invisible
		{ID: 1147, Value: 0},    // isVenom_playable
		{ID: 1149, Value: 20},   // get_ud_break_parts_point
		{ID: 1152, Value: 1130}, // unused?
		{ID: 1154, Value: 0},    // isDisabled_object_season
		{ID: 1158, Value: 1},    // isDelivery_venom_ult_quest
		{ID: 1160, Value: 300},  // get_rate_premium_ravi_g_enhance_tama

		// unknown
		{ID: 1162, Value: 1},
		{ID: 1163, Value: 3},
		{ID: 1164, Value: 5},
		{ID: 1165, Value: 1},
		{ID: 1166, Value: 5},
		{ID: 1167, Value: 1},
		{ID: 1168, Value: 3},
		{ID: 1169, Value: 3},
		{ID: 1170, Value: 5},
		{ID: 1171, Value: 1},
		{ID: 1172, Value: 1},
		{ID: 1173, Value: 1},
		{ID: 1174, Value: 2},
		{ID: 1175, Value: 4},
		{ID: 1176, Value: 10},
		{ID: 1177, Value: 4},
		{ID: 1178, Value: 10},
		{ID: 1179, Value: 2},
		{ID: 1180, Value: 5},
	}

	tuneValues = append(tuneValues, tuneValue{1020, uint16(s.server.erupeConfig.GameplayOptions.GCPMultiplier * 100)})

	tuneValues = append(tuneValues, tuneValue{1029, uint16(s.server.erupeConfig.GameplayOptions.GUrgentRate * 100)})

	if s.server.erupeConfig.GameplayOptions.DisableHunterNavi {
		tuneValues = append(tuneValues, tuneValue{1037, 1})
	}

	if s.server.erupeConfig.GameplayOptions.EnableKaijiEvent {
		tuneValues = append(tuneValues, tuneValue{1106, 1})
	}

	if s.server.erupeConfig.GameplayOptions.EnableHiganjimaEvent {
		tuneValues = append(tuneValues, tuneValue{1144, 1})
	}

	if s.server.erupeConfig.GameplayOptions.EnableNierEvent {
		tuneValues = append(tuneValues, tuneValue{1153, 1})
	}

	if s.server.erupeConfig.GameplayOptions.DisableRoad {
		tuneValues = append(tuneValues, tuneValue{1155, 1})
	}

	// get_hrp_rate_from_rank
	tuneValues = append(tuneValues, getTuneValueRange(3000, uint16(s.server.erupeConfig.GameplayOptions.HRPMultiplier*100))...)
	tuneValues = append(tuneValues, getTuneValueRange(3338, uint16(s.server.erupeConfig.GameplayOptions.HRPMultiplierNC*100))...)
	// get_srp_rate_from_rank
	tuneValues = append(tuneValues, getTuneValueRange(3013, uint16(s.server.erupeConfig.GameplayOptions.SRPMultiplier*100))...)
	tuneValues = append(tuneValues, getTuneValueRange(3351, uint16(s.server.erupeConfig.GameplayOptions.SRPMultiplierNC*100))...)
	// get_grp_rate_from_rank
	tuneValues = append(tuneValues, getTuneValueRange(3026, uint16(s.server.erupeConfig.GameplayOptions.GRPMultiplier*100))...)
	tuneValues = append(tuneValues, getTuneValueRange(3364, uint16(s.server.erupeConfig.GameplayOptions.GRPMultiplierNC*100))...)
	// get_gsrp_rate_from_rank
	tuneValues = append(tuneValues, getTuneValueRange(3039, uint16(s.server.erupeConfig.GameplayOptions.GSRPMultiplier*100))...)
	tuneValues = append(tuneValues, getTuneValueRange(3377, uint16(s.server.erupeConfig.GameplayOptions.GSRPMultiplierNC*100))...)
	// get_zeny_rate_from_hrank
	tuneValues = append(tuneValues, getTuneValueRange(3052, uint16(s.server.erupeConfig.GameplayOptions.ZennyMultiplier*100))...)
	tuneValues = append(tuneValues, getTuneValueRange(3390, uint16(s.server.erupeConfig.GameplayOptions.ZennyMultiplierNC*100))...)
	// get_zeny_rate_from_grank
	tuneValues = append(tuneValues, getTuneValueRange(3078, uint16(s.server.erupeConfig.GameplayOptions.GZennyMultiplier*100))...)
	tuneValues = append(tuneValues, getTuneValueRange(3416, uint16(s.server.erupeConfig.GameplayOptions.GZennyMultiplierNC*100))...)
	// get_reward_rate_from_hrank
	tuneValues = append(tuneValues, getTuneValueRange(3104, uint16(s.server.erupeConfig.GameplayOptions.MaterialMultiplier*100))...)
	tuneValues = append(tuneValues, getTuneValueRange(3442, uint16(s.server.erupeConfig.GameplayOptions.MaterialMultiplierNC*100))...)
	// get_reward_rate_from_grank
	tuneValues = append(tuneValues, getTuneValueRange(3130, uint16(s.server.erupeConfig.GameplayOptions.GMaterialMultiplier*100))...)
	tuneValues = append(tuneValues, getTuneValueRange(3468, uint16(s.server.erupeConfig.GameplayOptions.GMaterialMultiplierNC*100))...)
	// get_lottery_rate_from_hrank
	tuneValues = append(tuneValues, getTuneValueRange(3156, 0)...)
	tuneValues = append(tuneValues, getTuneValueRange(3494, 0)...)
	// get_lottery_rate_from_grank
	tuneValues = append(tuneValues, getTuneValueRange(3182, 0)...)
	tuneValues = append(tuneValues, getTuneValueRange(3520, 0)...)
	// get_hagi_rate_from_hrank
	tuneValues = append(tuneValues, getTuneValueRange(3208, s.server.erupeConfig.GameplayOptions.ExtraCarves)...)
	tuneValues = append(tuneValues, getTuneValueRange(3546, s.server.erupeConfig.GameplayOptions.ExtraCarvesNC)...)
	// get_hagi_rate_from_grank
	tuneValues = append(tuneValues, getTuneValueRange(3234, s.server.erupeConfig.GameplayOptions.GExtraCarves)...)
	tuneValues = append(tuneValues, getTuneValueRange(3572, s.server.erupeConfig.GameplayOptions.GExtraCarvesNC)...)
	// get_nboost_transcend_rate_from_hrank
	tuneValues = append(tuneValues, getTuneValueRange(3286, 200)...)
	tuneValues = append(tuneValues, getTuneValueRange(3312, 300)...)
	// get_nboost_transcend_rate_from_grank
	tuneValues = append(tuneValues, getTuneValueRange(3299, 200)...)
	tuneValues = append(tuneValues, getTuneValueRange(3325, 300)...)

	var temp []tuneValue
	for i := range tuneValues {
		if tuneValues[i].Value > 0 {
			temp = append(temp, tuneValues[i])
		}
	}
	tuneValues = temp

	tuneLimit := 770
	if _config.ErupeConfig.RealClientMode <= _config.G1 {
		tuneLimit = 256
	} else if _config.ErupeConfig.RealClientMode <= _config.G3 {
		tuneLimit = 283
	} else if _config.ErupeConfig.RealClientMode <= _config.GG {
		tuneLimit = 315
	} else if _config.ErupeConfig.RealClientMode <= _config.G61 {
		tuneLimit = 332
	} else if _config.ErupeConfig.RealClientMode <= _config.G7 {
		tuneLimit = 339
	} else if _config.ErupeConfig.RealClientMode <= _config.G81 {
		tuneLimit = 396
	} else if _config.ErupeConfig.RealClientMode <= _config.G91 {
		tuneLimit = 694
	} else if _config.ErupeConfig.RealClientMode <= _config.G101 {
		tuneLimit = 704
	} else if _config.ErupeConfig.RealClientMode <= _config.Z2 {
		tuneLimit = 750
	}
	if len(tuneValues) > tuneLimit {
		tuneValues = tuneValues[:tuneLimit]
	}

	offset := uint16(time.Now().Unix())
	bf.WriteUint16(offset)

	bf.WriteUint16(uint16(len(tuneValues)))
	for i := range tuneValues {
		bf.WriteUint16(tuneValues[i].ID ^ offset)
		bf.WriteUint16(offset)
		bf.WriteBytes(make([]byte, 4))
		bf.WriteUint16(tuneValues[i].Value ^ offset)
	}

	vsQuestItems := []uint16{1580, 1581, 1582, 1583, 1584, 1585, 1587, 1588, 1589, 1595, 1596, 1597, 1598, 1599, 1600, 1601, 1602, 1603, 1604}
	vsQuestBets := []struct {
		IsTicket bool
		Quantity uint32
	}{
		{true, 5},
		{false, 1000},
		{false, 5000},
		{false, 10000},
	}
	bf.WriteUint16(uint16(len(vsQuestItems)))
	bf.WriteUint16(0) // Unk array of uint16s
	bf.WriteUint16(uint16(len(vsQuestBets)))
	bf.WriteUint16(0) // Unk

	for i := range vsQuestItems {
		bf.WriteUint16(vsQuestItems[i])
	}
	for i := range vsQuestBets {
		bf.WriteBool(vsQuestBets[i].IsTicket)
		bf.WriteUint8(9)
		bf.WriteUint16(7)
		bf.WriteUint32(vsQuestBets[i].Quantity)
	}

	bf.WriteUint16(totalCount)
	bf.WriteUint16(pkt.Offset)
	bf.Seek(0, io.SeekStart)
	bf.WriteUint16(returnedCount)

	doAckBufSucceed(s, pkt.AckHandle, bf.Data())
}

func getTuneValueRange(start uint16, value uint16) []tuneValue {
	var tv []tuneValue
	for i := uint16(0); i < 13; i++ {
		tv = append(tv, tuneValue{start + i, value})
	}
	return tv
}

func handleMsgMhfEnterTournamentQuest(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgMhfGetUdBonusQuestInfo(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfGetUdBonusQuestInfo)

	udBonusQuestInfos := []struct {
		Unk0      uint8
		Unk1      uint8
		StartTime uint32 // Unix timestamp (seconds)
		EndTime   uint32 // Unix timestamp (seconds)
		Unk4      uint32
		Unk5      uint8
		Unk6      uint8
	}{} // Blank stub array.

	resp := byteframe.NewByteFrame()
	resp.WriteUint8(uint8(len(udBonusQuestInfos)))
	for _, q := range udBonusQuestInfos {
		resp.WriteUint8(q.Unk0)
		resp.WriteUint8(q.Unk1)
		resp.WriteUint32(q.StartTime)
		resp.WriteUint32(q.EndTime)
		resp.WriteUint32(q.Unk4)
		resp.WriteUint8(q.Unk5)
		resp.WriteUint8(q.Unk6)
	}

	doAckBufSucceed(s, pkt.AckHandle, resp.Data())
}
