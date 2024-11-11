package channelserver

import (
	"encoding/binary"
	"erupe-ce/common/mhfcourse"
	"erupe-ce/common/mhfitem"
	"erupe-ce/common/mhfmon"
	ps "erupe-ce/common/pascalstring"
	"erupe-ce/common/stringsupport"
	_config "erupe-ce/config"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"crypto/rand"
	"erupe-ce/common/byteframe"
	"erupe-ce/network/mhfpacket"
	"math/bits"

	"go.uber.org/zap"
)

// Temporary function to just return no results for a MSG_MHF_ENUMERATE* packet
func stubEnumerateNoResults(s *Session, ackHandle uint32) {
	enumBf := byteframe.NewByteFrame()
	enumBf.WriteUint32(0) // Entry count (count for quests, rankings, events, etc.)

	doAckBufSucceed(s, ackHandle, enumBf.Data())
}

func doAckEarthSucceed(s *Session, ackHandle uint32, data []*byteframe.ByteFrame) {
	bf := byteframe.NewByteFrame()
	bf.WriteUint32(uint32(s.server.erupeConfig.EarthID))
	bf.WriteUint32(0)
	bf.WriteUint32(0)
	bf.WriteUint32(uint32(len(data)))
	for i := range data {
		bf.WriteBytes(data[i].Data())
	}
	doAckBufSucceed(s, ackHandle, bf.Data())
}

func doAckBufSucceed(s *Session, ackHandle uint32, data []byte) {
	s.QueueSendMHF(&mhfpacket.MsgSysAck{
		AckHandle:        ackHandle,
		IsBufferResponse: true,
		ErrorCode:        0,
		AckData:          data,
	})
}

func doAckBufFail(s *Session, ackHandle uint32, data []byte) {
	s.QueueSendMHF(&mhfpacket.MsgSysAck{
		AckHandle:        ackHandle,
		IsBufferResponse: true,
		ErrorCode:        1,
		AckData:          data,
	})
}

func doAckSimpleSucceed(s *Session, ackHandle uint32, data []byte) {
	s.QueueSendMHF(&mhfpacket.MsgSysAck{
		AckHandle:        ackHandle,
		IsBufferResponse: false,
		ErrorCode:        0,
		AckData:          data,
	})
}

func doAckSimpleFail(s *Session, ackHandle uint32, data []byte) {
	s.QueueSendMHF(&mhfpacket.MsgSysAck{
		AckHandle:        ackHandle,
		IsBufferResponse: false,
		ErrorCode:        1,
		AckData:          data,
	})
}

func updateRights(s *Session) {
	rightsInt := uint32(2)
	s.server.db.QueryRow("SELECT rights FROM users u INNER JOIN characters c ON u.id = c.user_id WHERE c.id = $1", s.charID).Scan(&rightsInt)
	s.courses, rightsInt = mhfcourse.GetCourseStruct(rightsInt)
	update := &mhfpacket.MsgSysUpdateRight{
		ClientRespAckHandle: 0,
		Bitfield:            rightsInt,
		Rights:              s.courses,
		UnkSize:             0,
	}
	s.QueueSendMHF(update)
}

func handleMsgHead(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgSysExtendThreshold(s *Session, p mhfpacket.MHFPacket) {
	// No data aside from header, no resp required.
}

func handleMsgSysEnd(s *Session, p mhfpacket.MHFPacket) {
	// No data aside from header, no resp required.
}

func handleMsgSysNop(s *Session, p mhfpacket.MHFPacket) {
	// No data aside from header, no resp required.
}

func handleMsgSysAck(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgSysTerminalLog(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgSysTerminalLog)
	for i := range pkt.Entries {
		s.server.logger.Info("SysTerminalLog",
			zap.Uint8("Type1", pkt.Entries[i].Type1),
			zap.Uint8("Type2", pkt.Entries[i].Type2),
			zap.Int16("Unk0", pkt.Entries[i].Unk0),
			zap.Int32("Unk1", pkt.Entries[i].Unk1),
			zap.Int32("Unk2", pkt.Entries[i].Unk2),
			zap.Int32("Unk3", pkt.Entries[i].Unk3),
			zap.Int32s("Unk4", pkt.Entries[i].Unk4),
		)
	}
	resp := byteframe.NewByteFrame()
	resp.WriteUint32(pkt.LogID + 1) // LogID to use for requests after this.
	doAckSimpleSucceed(s, pkt.AckHandle, resp.Data())
}

func handleMsgSysLogin(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgSysLogin)

	if !s.server.erupeConfig.DebugOptions.DisableTokenCheck {
		var token string
		err := s.server.db.QueryRow("SELECT token FROM sign_sessions ss INNER JOIN public.users u on ss.user_id = u.id WHERE token=$1 AND ss.id=$2 AND u.id=(SELECT c.user_id FROM characters c WHERE c.id=$3)", pkt.LoginTokenString, pkt.LoginTokenNumber, pkt.CharID0).Scan(&token)
		if err != nil {
			s.rawConn.Close()
			s.logger.Warn(fmt.Sprintf("Invalid login token, offending CID: (%d)", pkt.CharID0))
			return
		}
	}

	s.Lock()
	s.charID = pkt.CharID0
	s.token = pkt.LoginTokenString
	s.Unlock()

	bf := byteframe.NewByteFrame()
	bf.WriteUint32(uint32(TimeAdjusted().Unix())) // Unix timestamp

	_, err := s.server.db.Exec("UPDATE servers SET current_players=$1 WHERE server_id=$2", len(s.server.sessions), s.server.ID)
	if err != nil {
		panic(err)
	}

	_, err = s.server.db.Exec("UPDATE sign_sessions SET server_id=$1, char_id=$2 WHERE token=$3", s.server.ID, s.charID, s.token)
	if err != nil {
		panic(err)
	}

	_, err = s.server.db.Exec("UPDATE characters SET last_login=$1 WHERE id=$2", TimeAdjusted().Unix(), s.charID)
	if err != nil {
		panic(err)
	}

	_, err = s.server.db.Exec("UPDATE users u SET last_character=$1 WHERE u.id=(SELECT c.user_id FROM characters c WHERE c.id=$1)", s.charID)
	if err != nil {
		panic(err)
	}

	doAckSimpleSucceed(s, pkt.AckHandle, bf.Data())

	updateRights(s)

	s.server.BroadcastMHF(&mhfpacket.MsgSysInsertUser{CharID: s.charID}, s)
}

func handleMsgSysLogout(s *Session, p mhfpacket.MHFPacket) {
	logoutPlayer(s)
}

func logoutPlayer(s *Session) {
	s.server.Lock()
	if _, exists := s.server.sessions[s.rawConn]; exists {
		delete(s.server.sessions, s.rawConn)
	}
	s.rawConn.Close()
	delete(s.server.objectIDs, s)
	s.server.Unlock()

	for _, stage := range s.server.stages {
		// Tell sessions registered to disconnecting players quest to unregister
		if stage.host != nil && stage.host.charID == s.charID {
			for _, sess := range s.server.sessions {
				for rSlot := range stage.reservedClientSlots {
					if sess.charID == rSlot && sess.stage != nil && sess.stage.id[3:5] != "Qs" {
						sess.QueueSendMHF(&mhfpacket.MsgSysStageDestruct{})
					}
				}
			}
		}
		for session := range stage.clients {
			if session.charID == s.charID {
				delete(stage.clients, session)
			}
		}
	}

	_, err := s.server.db.Exec("UPDATE sign_sessions SET server_id=NULL, char_id=NULL WHERE token=$1", s.token)
	if err != nil {
		panic(err)
	}

	_, err = s.server.db.Exec("UPDATE servers SET current_players=$1 WHERE server_id=$2", len(s.server.sessions), s.server.ID)
	if err != nil {
		panic(err)
	}

	var timePlayed int
	var sessionTime int
	_ = s.server.db.QueryRow("SELECT time_played FROM characters WHERE id = $1", s.charID).Scan(&timePlayed)
	sessionTime = int(TimeAdjusted().Unix()) - int(s.sessionStart)
	timePlayed += sessionTime

	var rpGained int
	if mhfcourse.CourseExists(30, s.courses) {
		rpGained = timePlayed / 900
		timePlayed = timePlayed % 900
		s.server.db.Exec("UPDATE characters SET cafe_time=cafe_time+$1 WHERE id=$2", sessionTime, s.charID)
	} else {
		rpGained = timePlayed / 1800
		timePlayed = timePlayed % 1800
	}

	s.server.db.Exec("UPDATE characters SET time_played = $1 WHERE id = $2", timePlayed, s.charID)

	s.server.db.Exec(`UPDATE guild_characters SET treasure_hunt=NULL WHERE character_id=$1`, s.charID)

	if s.stage == nil {
		return
	}

	s.server.BroadcastMHF(&mhfpacket.MsgSysDeleteUser{
		CharID: s.charID,
	}, s)

	s.server.Lock()
	for _, stage := range s.server.stages {
		if _, exists := stage.reservedClientSlots[s.charID]; exists {
			delete(stage.reservedClientSlots, s.charID)
		}
	}
	s.server.Unlock()

	removeSessionFromSemaphore(s)
	removeSessionFromStage(s)

	saveData, err := GetCharacterSaveData(s, s.charID)
	if err != nil || saveData == nil {
		s.logger.Error("Failed to get savedata")
		return
	}
	saveData.RP += uint16(rpGained)
	if saveData.RP >= s.server.erupeConfig.GameplayOptions.MaximumRP {
		saveData.RP = s.server.erupeConfig.GameplayOptions.MaximumRP
	}
	saveData.Save(s)
}

func handleMsgSysSetStatus(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgSysPing(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgSysPing)
	doAckSimpleSucceed(s, pkt.AckHandle, []byte{0x00, 0x00, 0x00, 0x00})
}

func handleMsgSysTime(s *Session, p mhfpacket.MHFPacket) {
	resp := &mhfpacket.MsgSysTime{
		GetRemoteTime: false,
		Timestamp:     uint32(TimeAdjusted().Unix()), // JP timezone
	}
	s.QueueSendMHF(resp)
	s.notifyRavi()
}

func handleMsgSysIssueLogkey(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgSysIssueLogkey)

	// Make a random log key for this session.
	logKey := make([]byte, 16)
	_, err := rand.Read(logKey)
	if err != nil {
		panic(err)
	}

	// TODO(Andoryuuta): In the offical client, the log key index is off by one,
	// cutting off the last byte in _most uses_. Find and document these accordingly.
	s.Lock()
	s.logKey = logKey
	s.Unlock()

	// Issue it.
	resp := byteframe.NewByteFrame()
	resp.WriteBytes(logKey)
	doAckBufSucceed(s, pkt.AckHandle, resp.Data())
}

func handleMsgSysRecordLog(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgSysRecordLog)
	if _config.ErupeConfig.RealClientMode == _config.ZZ {
		bf := byteframe.NewByteFrameFromBytes(pkt.Data)
		bf.Seek(32, 0)
		var val uint8
		for i := 0; i < 176; i++ {
			val = bf.ReadUint8()
			if val > 0 && mhfmon.Monsters[i].Large {
				s.server.db.Exec(`INSERT INTO kill_logs (character_id, monster, quantity, timestamp) VALUES ($1, $2, $3, $4)`, s.charID, i, val, TimeAdjusted())
			}
		}
	}
	// remove a client returning to town from reserved slots to make sure the stage is hidden from board
	delete(s.stage.reservedClientSlots, s.charID)
	doAckSimpleSucceed(s, pkt.AckHandle, make([]byte, 4))
}

func handleMsgSysEcho(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgSysLockGlobalSema(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgSysLockGlobalSema)
	var sgid string
	for _, channel := range s.server.Channels {
		for id := range channel.stages {
			if strings.HasSuffix(id, pkt.UserIDString) {
				sgid = channel.GlobalID
			}
		}
	}
	bf := byteframe.NewByteFrame()
	if len(sgid) > 0 && sgid != s.server.GlobalID {
		bf.WriteUint8(0)
		bf.WriteUint8(0)
		ps.Uint16(bf, sgid, false)
	} else {
		bf.WriteUint8(2)
		bf.WriteUint8(0)
		ps.Uint16(bf, pkt.ServerChannelIDString, false)
	}
	doAckBufSucceed(s, pkt.AckHandle, bf.Data())
}

func handleMsgSysUnlockGlobalSema(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgSysUnlockGlobalSema)
	doAckSimpleSucceed(s, pkt.AckHandle, make([]byte, 4))
}

func handleMsgSysUpdateRight(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgSysAuthQuery(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgSysAuthTerminal(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgSysRightsReload(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgSysRightsReload)
	updateRights(s)
	doAckSimpleSucceed(s, pkt.AckHandle, []byte{0x00, 0x00, 0x00, 0x00})
}

func handleMsgMhfTransitMessage(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfTransitMessage)

	local := false
	if strings.Split(s.rawConn.RemoteAddr().String(), ":")[0] == "127.0.0.1" {
		local = true
	}

	var maxResults, port, count uint16
	var cid uint32
	var term, ip string
	bf := byteframe.NewByteFrameFromBytes(pkt.MessageData)
	switch pkt.SearchType {
	case 1:
		maxResults = 1
		cid = bf.ReadUint32()
	case 2:
		bf.ReadUint16() // term length
		maxResults = bf.ReadUint16()
		bf.ReadUint8() // Unk
		term = stringsupport.SJISToUTF8(bf.ReadNullTerminatedBytes())
	case 3:
		_ip := bf.ReadBytes(4)
		ip = fmt.Sprintf("%d.%d.%d.%d", _ip[3], _ip[2], _ip[1], _ip[0])
		port = bf.ReadUint16()
		bf.ReadUint16() // term length
		maxResults = bf.ReadUint16()
		bf.ReadUint8()
		term = string(bf.ReadNullTerminatedBytes())
	}

	resp := byteframe.NewByteFrame()
	resp.WriteUint16(0)
	switch pkt.SearchType {
	case 1, 2, 3: // usersearchidx, usersearchname, lobbysearchname
		for _, c := range s.server.Channels {
			for _, session := range c.sessions {
				if count == maxResults {
					break
				}
				if pkt.SearchType == 1 && session.charID != cid {
					continue
				}
				if pkt.SearchType == 2 && !strings.Contains(session.Name, term) {
					continue
				}
				if pkt.SearchType == 3 && session.server.IP != ip && session.server.Port != port && session.stage.id != term {
					continue
				}
				count++
				sessionName := stringsupport.UTF8ToSJIS(session.Name)
				sessionStage := stringsupport.UTF8ToSJIS(session.stage.id)
				if !local {
					resp.WriteUint32(binary.LittleEndian.Uint32(net.ParseIP(c.IP).To4()))
				} else {
					resp.WriteUint32(0x0100007F)
				}
				resp.WriteUint16(c.Port)
				resp.WriteUint32(session.charID)
				resp.WriteUint8(uint8(len(sessionStage) + 1))
				resp.WriteUint8(uint8(len(sessionName) + 1))
				resp.WriteUint16(uint16(len(c.userBinaryParts[userBinaryPartID{charID: session.charID, index: 3}])))

				// TODO: This case might be <=G2
				if _config.ErupeConfig.RealClientMode <= _config.G1 {
					resp.WriteBytes(make([]byte, 8))
				} else {
					resp.WriteBytes(make([]byte, 40))
				}
				resp.WriteBytes(make([]byte, 8))

				resp.WriteNullTerminatedBytes(sessionStage)
				resp.WriteNullTerminatedBytes(sessionName)
				resp.WriteBytes(c.userBinaryParts[userBinaryPartID{session.charID, 3}])
			}
		}
	case 4: // lobbysearch
		type FindPartyParams struct {
			StagePrefix     string
			RankRestriction int16
			Targets         []int16
			Unk0            []int16
			Unk1            []int16
			QuestID         []int16
		}
		findPartyParams := FindPartyParams{
			StagePrefix: "sl2Ls210",
		}
		numParams := bf.ReadUint8()
		maxResults = bf.ReadUint16()
		for i := uint8(0); i < numParams; i++ {
			switch bf.ReadUint8() {
			case 0:
				values := bf.ReadUint8()
				for i := uint8(0); i < values; i++ {
					if _config.ErupeConfig.RealClientMode >= _config.Z1 {
						findPartyParams.RankRestriction = bf.ReadInt16()
					} else {
						findPartyParams.RankRestriction = int16(bf.ReadInt8())
					}
				}
			case 1:
				values := bf.ReadUint8()
				for i := uint8(0); i < values; i++ {
					if _config.ErupeConfig.RealClientMode >= _config.Z1 {
						findPartyParams.Targets = append(findPartyParams.Targets, bf.ReadInt16())
					} else {
						findPartyParams.Targets = append(findPartyParams.Targets, int16(bf.ReadInt8()))
					}
				}
			case 2:
				values := bf.ReadUint8()
				for i := uint8(0); i < values; i++ {
					var value int16
					if _config.ErupeConfig.RealClientMode >= _config.Z1 {
						value = bf.ReadInt16()
					} else {
						value = int16(bf.ReadInt8())
					}
					switch value {
					case 0: // Public Bar
						findPartyParams.StagePrefix = "sl2Ls210"
					case 1: // Tokotoko Partnya
						findPartyParams.StagePrefix = "sl2Ls463"
					case 2: // Hunting Prowess Match
						findPartyParams.StagePrefix = "sl2Ls286"
					case 3: // Volpakkun Together
						findPartyParams.StagePrefix = "sl2Ls465"
					case 5: // Quick Party
						// Unk
					}
				}
			case 3: // Unknown
				values := bf.ReadUint8()
				for i := uint8(0); i < values; i++ {
					if _config.ErupeConfig.RealClientMode >= _config.Z1 {
						findPartyParams.Unk0 = append(findPartyParams.Unk0, bf.ReadInt16())
					} else {
						findPartyParams.Unk0 = append(findPartyParams.Unk0, int16(bf.ReadInt8()))
					}
				}
			case 4: // Looking for n or already have n
				values := bf.ReadUint8()
				for i := uint8(0); i < values; i++ {
					if _config.ErupeConfig.RealClientMode >= _config.Z1 {
						findPartyParams.Unk1 = append(findPartyParams.Unk1, bf.ReadInt16())
					} else {
						findPartyParams.Unk1 = append(findPartyParams.Unk1, int16(bf.ReadInt8()))
					}
				}
			case 5:
				values := bf.ReadUint8()
				for i := uint8(0); i < values; i++ {
					if _config.ErupeConfig.RealClientMode >= _config.Z1 {
						findPartyParams.QuestID = append(findPartyParams.QuestID, bf.ReadInt16())
					} else {
						findPartyParams.QuestID = append(findPartyParams.QuestID, int16(bf.ReadInt8()))
					}
				}
			}
		}
		for _, c := range s.server.Channels {
			for _, stage := range c.stages {
				if count == maxResults {
					break
				}
				if strings.HasPrefix(stage.id, findPartyParams.StagePrefix) {
					sb3 := byteframe.NewByteFrameFromBytes(stage.rawBinaryData[stageBinaryKey{1, 3}])
					sb3.Seek(4, 0)

					stageDataParams := 7
					if _config.ErupeConfig.RealClientMode <= _config.G10 {
						stageDataParams = 4
					} else if _config.ErupeConfig.RealClientMode <= _config.Z1 {
						stageDataParams = 6
					}

					var stageData []int16
					for i := 0; i < stageDataParams; i++ {
						if _config.ErupeConfig.RealClientMode >= _config.Z1 {
							stageData = append(stageData, sb3.ReadInt16())
						} else {
							stageData = append(stageData, int16(sb3.ReadInt8()))
						}
					}

					if findPartyParams.RankRestriction >= 0 {
						if stageData[0] > findPartyParams.RankRestriction {
							continue
						}
					}

					var hasTarget bool
					if len(findPartyParams.Targets) > 0 {
						for _, target := range findPartyParams.Targets {
							if target == stageData[1] {
								hasTarget = true
								break
							}
						}
						if !hasTarget {
							continue
						}
					}

					count++
					if !local {
						resp.WriteUint32(binary.LittleEndian.Uint32(net.ParseIP(c.IP).To4()))
					} else {
						resp.WriteUint32(0x0100007F)
					}
					resp.WriteUint16(c.Port)

					resp.WriteUint16(0) // Static?
					resp.WriteUint16(0) // Unk, [0 1 2]
					resp.WriteUint16(uint16(len(stage.clients) + len(stage.reservedClientSlots)))
					resp.WriteUint16(stage.maxPlayers)
					// TODO: Retail returned the number of clients in quests, not workshop/my series
					resp.WriteUint16(uint16(len(stage.reservedClientSlots)))

					resp.WriteUint8(0) // Static?
					resp.WriteUint8(uint8(stage.maxPlayers))
					resp.WriteUint8(1) // Static?
					resp.WriteUint8(uint8(len(stage.id) + 1))
					resp.WriteUint8(uint8(len(stage.rawBinaryData[stageBinaryKey{1, 0}])))
					resp.WriteUint8(uint8(len(stage.rawBinaryData[stageBinaryKey{1, 1}])))

					for i := range stageData {
						if _config.ErupeConfig.RealClientMode >= _config.Z1 {
							resp.WriteInt16(stageData[i])
						} else {
							resp.WriteInt8(int8(stageData[i]))
						}
					}
					resp.WriteUint8(0) // Unk
					resp.WriteUint8(0) // Unk

					resp.WriteNullTerminatedBytes([]byte(stage.id))
					resp.WriteBytes(stage.rawBinaryData[stageBinaryKey{1, 0}])
					resp.WriteBytes(stage.rawBinaryData[stageBinaryKey{1, 1}])
				}
			}
		}
	}
	resp.Seek(0, io.SeekStart)
	resp.WriteUint16(count)
	doAckBufSucceed(s, pkt.AckHandle, resp.Data())
}

func handleMsgCaExchangeItem(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgMhfServerCommand(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgMhfAnnounce(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfAnnounce)
	s.server.BroadcastRaviente(pkt.IPAddress, pkt.Port, pkt.StageID, pkt.Data.ReadUint8())
	doAckSimpleSucceed(s, pkt.AckHandle, make([]byte, 4))
}

func handleMsgMhfSetLoginwindow(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgSysTransBinary(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgSysCollectBinary(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgSysGetState(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgSysSerialize(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgSysEnumlobby(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgSysEnumuser(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgSysInfokyserver(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgMhfGetCaUniqueID(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgMhfEnumeratePrice(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfEnumeratePrice)
	bf := byteframe.NewByteFrame()
	var lbPrices []struct {
		Unk0 uint16
		Unk1 uint16
		Unk2 uint32
	}
	var wantedList []struct {
		Unk0 uint32
		Unk1 uint32
		Unk2 uint32
		Unk3 uint16
		Unk4 uint16
		Unk5 uint16
		Unk6 uint16
		Unk7 uint16
		Unk8 uint16
		Unk9 uint16
	}
	gzPrices := []struct {
		Unk0  uint16
		Gz    uint16
		Unk1  uint16
		Unk2  uint16
		MonID uint16
		Unk3  uint16
		Unk4  uint8
	}{
		{0, 1000, 0, 0, mhfmon.Pokaradon, 100, 1},
		{0, 800, 0, 0, mhfmon.YianKutKu, 100, 1},
		{0, 800, 0, 0, mhfmon.DaimyoHermitaur, 100, 1},
		{0, 1100, 0, 0, mhfmon.Farunokku, 100, 1},
		{0, 900, 0, 0, mhfmon.Congalala, 100, 1},
		{0, 900, 0, 0, mhfmon.Gypceros, 100, 1},
		{0, 1300, 0, 0, mhfmon.Hyujikiki, 100, 1},
		{0, 1000, 0, 0, mhfmon.Basarios, 100, 1},
		{0, 1000, 0, 0, mhfmon.Rathian, 100, 1},
		{0, 800, 0, 0, mhfmon.ShogunCeanataur, 100, 1},
		{0, 1400, 0, 0, mhfmon.Midogaron, 100, 1},
		{0, 900, 0, 0, mhfmon.Blangonga, 100, 1},
		{0, 1100, 0, 0, mhfmon.Rathalos, 100, 1},
		{0, 1000, 0, 0, mhfmon.Khezu, 100, 1},
		{0, 1600, 0, 0, mhfmon.Giaorugu, 100, 1},
		{0, 1100, 0, 0, mhfmon.Gravios, 100, 1},
		{0, 1400, 0, 0, mhfmon.Tigrex, 100, 1},
		{0, 1000, 0, 0, mhfmon.Pariapuria, 100, 1},
		{0, 1700, 0, 0, mhfmon.Anorupatisu, 100, 1},
		{0, 1500, 0, 0, mhfmon.Lavasioth, 100, 1},
		{0, 1500, 0, 0, mhfmon.Espinas, 100, 1},
		{0, 1600, 0, 0, mhfmon.Rajang, 100, 1},
		{0, 1800, 0, 0, mhfmon.Rebidiora, 100, 1},
		{0, 1100, 0, 0, mhfmon.YianGaruga, 100, 1},
		{0, 1500, 0, 0, mhfmon.AqraVashimu, 100, 1},
		{0, 1600, 0, 0, mhfmon.Gurenzeburu, 100, 1},
		{0, 1500, 0, 0, mhfmon.Dyuragaua, 100, 1},
		{0, 1300, 0, 0, mhfmon.Gougarf, 100, 1},
		{0, 1000, 0, 0, mhfmon.Shantien, 100, 1},
		{0, 1800, 0, 0, mhfmon.Disufiroa, 100, 1},
		{0, 600, 0, 0, mhfmon.Velocidrome, 100, 1},
		{0, 600, 0, 0, mhfmon.Gendrome, 100, 1},
		{0, 700, 0, 0, mhfmon.Iodrome, 100, 1},
		{0, 1700, 0, 0, mhfmon.Baruragaru, 100, 1},
		{0, 800, 0, 0, mhfmon.Cephadrome, 100, 1},
		{0, 1000, 0, 0, mhfmon.Plesioth, 100, 1},
		{0, 1800, 0, 0, mhfmon.Zerureusu, 100, 1},
		{0, 1100, 0, 0, mhfmon.Diablos, 100, 1},
		{0, 1600, 0, 0, mhfmon.Berukyurosu, 100, 1},
		{0, 2000, 0, 0, mhfmon.Fatalis, 100, 1},
		{0, 1500, 0, 0, mhfmon.BlackGravios, 100, 1},
		{0, 1600, 0, 0, mhfmon.GoldRathian, 100, 1},
		{0, 1900, 0, 0, mhfmon.Meraginasu, 100, 1},
		{0, 700, 0, 0, mhfmon.Bulldrome, 100, 1},
		{0, 900, 0, 0, mhfmon.NonoOrugaron, 100, 1},
		{0, 1600, 0, 0, mhfmon.KamuOrugaron, 100, 1},
		{0, 1700, 0, 0, mhfmon.Forokururu, 100, 1},
		{0, 1900, 0, 0, mhfmon.Diorex, 100, 1},
		{0, 1500, 0, 0, mhfmon.AqraJebia, 100, 1},
		{0, 1600, 0, 0, mhfmon.SilverRathalos, 100, 1},
		{0, 2400, 0, 0, mhfmon.CrimsonFatalis, 100, 1},
		{0, 2000, 0, 0, mhfmon.Inagami, 100, 1},
		{0, 2100, 0, 0, mhfmon.GarubaDaora, 100, 1},
		{0, 900, 0, 0, mhfmon.Monoblos, 100, 1},
		{0, 1000, 0, 0, mhfmon.RedKhezu, 100, 1},
		{0, 900, 0, 0, mhfmon.Hypnocatrice, 100, 1},
		{0, 1700, 0, 0, mhfmon.PearlEspinas, 100, 1},
		{0, 900, 0, 0, mhfmon.PurpleGypceros, 100, 1},
		{0, 1800, 0, 0, mhfmon.Poborubarumu, 100, 1},
		{0, 1900, 0, 0, mhfmon.Lunastra, 100, 1},
		{0, 1600, 0, 0, mhfmon.Kuarusepusu, 100, 1},
		{0, 1100, 0, 0, mhfmon.PinkRathian, 100, 1},
		{0, 1200, 0, 0, mhfmon.AzureRathalos, 100, 1},
		{0, 1800, 0, 0, mhfmon.Varusaburosu, 100, 1},
		{0, 1000, 0, 0, mhfmon.Gogomoa, 100, 1},
		{0, 1600, 0, 0, mhfmon.BurningEspinas, 100, 1},
		{0, 2000, 0, 0, mhfmon.Harudomerugu, 100, 1},
		{0, 1800, 0, 0, mhfmon.Akantor, 100, 1},
		{0, 900, 0, 0, mhfmon.BrightHypnoc, 100, 1},
		{0, 2200, 0, 0, mhfmon.Gureadomosu, 100, 1},
		{0, 1200, 0, 0, mhfmon.GreenPlesioth, 100, 1},
		{0, 2400, 0, 0, mhfmon.Zinogre, 100, 1},
		{0, 1900, 0, 0, mhfmon.Gasurabazura, 100, 1},
		{0, 1300, 0, 0, mhfmon.Abiorugu, 100, 1},
		{0, 1200, 0, 0, mhfmon.BlackDiablos, 100, 1},
		{0, 1000, 0, 0, mhfmon.WhiteMonoblos, 100, 1},
		{0, 3000, 0, 0, mhfmon.Deviljho, 100, 1},
		{0, 2300, 0, 0, mhfmon.YamaKurai, 100, 1},
		{0, 2800, 0, 0, mhfmon.Brachydios, 100, 1},
		{0, 1700, 0, 0, mhfmon.Toridcless, 100, 1},
		{0, 1100, 0, 0, mhfmon.WhiteHypnoc, 100, 1},
		{0, 1500, 0, 0, mhfmon.RedLavasioth, 100, 1},
		{0, 2200, 0, 0, mhfmon.Barioth, 100, 1},
		{0, 1800, 0, 0, mhfmon.Odibatorasu, 100, 1},
		{0, 1600, 0, 0, mhfmon.Doragyurosu, 100, 1},
		{0, 900, 0, 0, mhfmon.BlueYianKutKu, 100, 1},
		{0, 2300, 0, 0, mhfmon.ToaTesukatora, 100, 1},
		{0, 2000, 0, 0, mhfmon.Uragaan, 100, 1},
		{0, 1900, 0, 0, mhfmon.Teostra, 100, 1},
		{0, 1700, 0, 0, mhfmon.Chameleos, 100, 1},
		{0, 1800, 0, 0, mhfmon.KushalaDaora, 100, 1},
		{0, 2100, 0, 0, mhfmon.Nargacuga, 100, 1},
		{0, 2600, 0, 0, mhfmon.Guanzorumu, 100, 1},
		{0, 1900, 0, 0, mhfmon.Kirin, 100, 1},
		{0, 2000, 0, 0, mhfmon.Rukodiora, 100, 1},
		{0, 2700, 0, 0, mhfmon.StygianZinogre, 100, 1},
		{0, 2200, 0, 0, mhfmon.Voljang, 100, 1},
		{0, 1800, 0, 0, mhfmon.Zenaserisu, 100, 1},
		{0, 3100, 0, 0, mhfmon.GoreMagala, 100, 1},
		{0, 3200, 0, 0, mhfmon.ShagaruMagala, 100, 1},
		{0, 3500, 0, 0, mhfmon.Eruzerion, 100, 1},
		{0, 3200, 0, 0, mhfmon.Amatsu, 100, 1},
	}

	bf.WriteUint16(uint16(len(lbPrices)))
	for _, lb := range lbPrices {
		bf.WriteUint16(lb.Unk0)
		bf.WriteUint16(lb.Unk1)
		bf.WriteUint32(lb.Unk2)
	}
	bf.WriteUint16(uint16(len(wantedList)))
	for _, wanted := range wantedList {
		bf.WriteUint32(wanted.Unk0)
		bf.WriteUint32(wanted.Unk1)
		bf.WriteUint32(wanted.Unk2)
		bf.WriteUint16(wanted.Unk3)
		bf.WriteUint16(wanted.Unk4)
		bf.WriteUint16(wanted.Unk5)
		bf.WriteUint16(wanted.Unk6)
		bf.WriteUint16(wanted.Unk7)
		bf.WriteUint16(wanted.Unk8)
		bf.WriteUint16(wanted.Unk9)
	}
	bf.WriteUint8(uint8(len(gzPrices)))
	for _, gz := range gzPrices {
		bf.WriteUint16(gz.Unk0)
		bf.WriteUint16(gz.Gz)
		bf.WriteUint16(gz.Unk1)
		bf.WriteUint16(gz.Unk2)
		bf.WriteUint16(gz.MonID)
		bf.WriteUint16(gz.Unk3)
		bf.WriteUint8(gz.Unk4)
	}
	doAckBufSucceed(s, pkt.AckHandle, bf.Data())
}

func handleMsgMhfEnumerateOrder(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfEnumerateOrder)
	stubEnumerateNoResults(s, pkt.AckHandle)
}

func handleMsgMhfGetExtraInfo(s *Session, p mhfpacket.MHFPacket) {}

func userGetItems(s *Session) []mhfitem.MHFItemStack {
	var data []byte
	var items []mhfitem.MHFItemStack
	s.server.db.QueryRow(`SELECT item_box FROM users u WHERE u.id=(SELECT c.user_id FROM characters c WHERE c.id=$1)`, s.charID).Scan(&data)
	if len(data) > 0 {
		box := byteframe.NewByteFrameFromBytes(data)
		numStacks := box.ReadUint16()
		box.ReadUint16() // Unused
		for i := 0; i < int(numStacks); i++ {
			items = append(items, mhfitem.ReadWarehouseItem(box))
		}
	}
	return items
}

func handleMsgMhfEnumerateUnionItem(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfEnumerateUnionItem)
	items := userGetItems(s)
	bf := byteframe.NewByteFrame()
	bf.WriteBytes(mhfitem.SerializeWarehouseItems(items))
	doAckBufSucceed(s, pkt.AckHandle, bf.Data())
}

func handleMsgMhfUpdateUnionItem(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfUpdateUnionItem)
	newStacks := mhfitem.DiffItemStacks(userGetItems(s), pkt.UpdatedItems)
	s.server.db.Exec(`UPDATE users u SET item_box=$1 WHERE u.id=(SELECT c.user_id FROM characters c WHERE c.id=$2)`, mhfitem.SerializeWarehouseItems(newStacks), s.charID)
	doAckSimpleSucceed(s, pkt.AckHandle, make([]byte, 4))
}

func handleMsgMhfGetCogInfo(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgMhfCheckWeeklyStamp(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfCheckWeeklyStamp)
	var total, redeemed, updated uint16
	var lastCheck time.Time
	err := s.server.db.QueryRow(fmt.Sprintf("SELECT %s_checked FROM stamps WHERE character_id=$1", pkt.StampType), s.charID).Scan(&lastCheck)
	if err != nil {
		lastCheck = TimeAdjusted()
		s.server.db.Exec("INSERT INTO stamps (character_id, hl_checked, ex_checked) VALUES ($1, $2, $2)", s.charID, TimeAdjusted())
	} else {
		s.server.db.Exec(fmt.Sprintf(`UPDATE stamps SET %s_checked=$1 WHERE character_id=$2`, pkt.StampType), TimeAdjusted(), s.charID)
	}

	if lastCheck.Before(TimeWeekStart()) {
		s.server.db.Exec(fmt.Sprintf("UPDATE stamps SET %s_total=%s_total+1 WHERE character_id=$1", pkt.StampType, pkt.StampType), s.charID)
		updated = 1
	}

	s.server.db.QueryRow(fmt.Sprintf("SELECT %s_total, %s_redeemed FROM stamps WHERE character_id=$1", pkt.StampType, pkt.StampType), s.charID).Scan(&total, &redeemed)
	bf := byteframe.NewByteFrame()
	bf.WriteUint16(total)
	bf.WriteUint16(redeemed)
	bf.WriteUint16(updated)
	bf.WriteUint16(0)
	bf.WriteUint16(0)
	bf.WriteUint32(uint32(TimeWeekStart().Unix()))
	doAckBufSucceed(s, pkt.AckHandle, bf.Data())
}

func handleMsgMhfExchangeWeeklyStamp(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfExchangeWeeklyStamp)
	var total, redeemed uint16
	var tktStack mhfitem.MHFItemStack
	if pkt.Unk1 == 10 { // Yearly Sub Ex
		s.server.db.QueryRow("UPDATE stamps SET hl_total=hl_total-48, hl_redeemed=hl_redeemed-48 WHERE character_id=$1 RETURNING hl_total, hl_redeemed", s.charID).Scan(&total, &redeemed)
		tktStack = mhfitem.MHFItemStack{Item: mhfitem.MHFItem{ItemID: 2210}, Quantity: 1}
	} else {
		s.server.db.QueryRow(fmt.Sprintf("UPDATE stamps SET %s_redeemed=%s_redeemed+8 WHERE character_id=$1 RETURNING %s_total, %s_redeemed", pkt.StampType, pkt.StampType, pkt.StampType, pkt.StampType), s.charID).Scan(&total, &redeemed)
		if pkt.StampType == "hl" {
			tktStack = mhfitem.MHFItemStack{Item: mhfitem.MHFItem{ItemID: 1630}, Quantity: 5}
		} else {
			tktStack = mhfitem.MHFItemStack{Item: mhfitem.MHFItem{ItemID: 1631}, Quantity: 5}
		}
	}
	addWarehouseItem(s, tktStack)
	bf := byteframe.NewByteFrame()
	bf.WriteUint16(total)
	bf.WriteUint16(redeemed)
	bf.WriteUint16(0)
	bf.WriteUint16(tktStack.Item.ItemID)
	bf.WriteUint16(tktStack.Quantity)
	bf.WriteUint32(uint32(TimeWeekStart().Unix()))
	doAckBufSucceed(s, pkt.AckHandle, bf.Data())
}

func getGoocooData(s *Session, cid uint32) [][]byte {
	var goocoo []byte
	var goocoos [][]byte
	for i := 0; i < 5; i++ {
		err := s.server.db.QueryRow(fmt.Sprintf("SELECT goocoo%d FROM goocoo WHERE id=$1", i), cid).Scan(&goocoo)
		if err != nil {
			s.server.db.Exec("INSERT INTO goocoo (id) VALUES ($1)", s.charID)
			return goocoos
		}
		if err == nil && goocoo != nil {
			goocoos = append(goocoos, goocoo)
		}
	}
	return goocoos
}

func handleMsgMhfEnumerateGuacot(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfEnumerateGuacot)
	bf := byteframe.NewByteFrame()
	goocoos := getGoocooData(s, s.charID)
	bf.WriteUint16(uint16(len(goocoos)))
	bf.WriteUint16(0)
	for _, goocoo := range goocoos {
		bf.WriteBytes(goocoo)
	}
	doAckBufSucceed(s, pkt.AckHandle, bf.Data())
}

func handleMsgMhfUpdateGuacot(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfUpdateGuacot)
	for _, goocoo := range pkt.Goocoos {
		if goocoo.Data1[0] == 0 {
			s.server.db.Exec(fmt.Sprintf("UPDATE goocoo SET goocoo%d=NULL WHERE id=$1", goocoo.Index), s.charID)
		} else {
			bf := byteframe.NewByteFrame()
			bf.WriteUint32(goocoo.Index)
			for i := range goocoo.Data1 {
				bf.WriteInt16(goocoo.Data1[i])
			}
			for i := range goocoo.Data2 {
				bf.WriteUint32(goocoo.Data2[i])
			}
			bf.WriteUint8(uint8(len(goocoo.Name)))
			bf.WriteBytes(goocoo.Name)
			s.server.db.Exec(fmt.Sprintf("UPDATE goocoo SET goocoo%d=$1 WHERE id=$2", goocoo.Index), bf.Data(), s.charID)
			dumpSaveData(s, bf.Data(), fmt.Sprintf("goocoo-%d", goocoo.Index))
		}
	}
	doAckSimpleSucceed(s, pkt.AckHandle, make([]byte, 4))
}

type Scenario struct {
	MainID uint32
	// 0 = Basic
	// 1 = Veteran
	// 3 = Other
	// 6 = Pallone
	// 7 = Diva
	CategoryID uint8
}

func handleMsgMhfInfoScenarioCounter(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfInfoScenarioCounter)
	var scenarios []Scenario
	var scenario Scenario
	scenarioData, err := s.server.db.Queryx("SELECT scenario_id, category_id FROM scenario_counter")
	if err != nil {
		scenarioData.Close()
		s.logger.Error("Failed to get scenario counter info from db", zap.Error(err))
		doAckBufSucceed(s, pkt.AckHandle, make([]byte, 1))
		return
	}
	for scenarioData.Next() {
		err = scenarioData.Scan(&scenario.MainID, &scenario.CategoryID)
		if err != nil {
			continue
		}
		scenarios = append(scenarios, scenario)
	}

	// Trim excess scenarios
	if len(scenarios) > 128 {
		scenarios = scenarios[:128]
	}

	bf := byteframe.NewByteFrame()
	bf.WriteUint8(uint8(len(scenarios)))
	for _, scenario := range scenarios {
		bf.WriteUint32(scenario.MainID)
		// If item exchange
		switch scenario.CategoryID {
		case 3, 6, 7:
			bf.WriteBool(true)
		default:
			bf.WriteBool(false)
		}
		bf.WriteUint8(scenario.CategoryID)
	}
	doAckBufSucceed(s, pkt.AckHandle, bf.Data())
}

func handleMsgMhfGetEtcPoints(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfGetEtcPoints)

	var dailyTime time.Time
	_ = s.server.db.QueryRow("SELECT COALESCE(daily_time, $2) FROM characters WHERE id = $1", s.charID, time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)).Scan(&dailyTime)
	if TimeAdjusted().After(dailyTime) {
		s.server.db.Exec("UPDATE characters SET bonus_quests = 0, daily_quests = 0 WHERE id=$1", s.charID)
	}

	var bonusQuests, dailyQuests, promoPoints uint32
	_ = s.server.db.QueryRow(`SELECT bonus_quests, daily_quests, promo_points FROM characters WHERE id = $1`, s.charID).Scan(&bonusQuests, &dailyQuests, &promoPoints)
	resp := byteframe.NewByteFrame()
	resp.WriteUint8(3) // Maybe a count of uint32(s)?
	resp.WriteUint32(bonusQuests)
	resp.WriteUint32(dailyQuests)
	resp.WriteUint32(promoPoints)
	doAckBufSucceed(s, pkt.AckHandle, resp.Data())
}

func handleMsgMhfUpdateEtcPoint(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfUpdateEtcPoint)

	var column string
	switch pkt.PointType {
	case 0:
		column = "bonus_quests"
	case 1:
		column = "daily_quests"
	case 2:
		column = "promo_points"
	}

	var value int16
	err := s.server.db.QueryRow(fmt.Sprintf(`SELECT %s FROM characters WHERE id = $1`, column), s.charID).Scan(&value)
	if err == nil {
		if value+pkt.Delta < 0 {
			s.server.db.Exec(fmt.Sprintf(`UPDATE characters SET %s = 0 WHERE id = $1`, column), s.charID)
		} else {
			s.server.db.Exec(fmt.Sprintf(`UPDATE characters SET %s = %s + $1 WHERE id = $2`, column, column), pkt.Delta, s.charID)
		}
	}
	doAckSimpleSucceed(s, pkt.AckHandle, make([]byte, 4))
}

func handleMsgMhfStampcardStamp(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfStampcardStamp)

	rewards := []struct {
		HR        uint16
		Item1     uint16
		Quantity1 uint16
		Item2     uint16
		Quantity2 uint16
	}{
		{0, 6164, 1, 6164, 2},
		{50, 6164, 2, 6164, 3},
		{100, 6164, 3, 5392, 1},
		{300, 5392, 1, 5392, 3},
		{999, 5392, 1, 5392, 4},
	}
	if _config.ErupeConfig.RealClientMode <= _config.Z1 {
		for _, reward := range rewards {
			if pkt.HR >= reward.HR {
				pkt.Item1 = reward.Item1
				pkt.Quantity1 = reward.Quantity1
				pkt.Item2 = reward.Item2
				pkt.Quantity2 = reward.Quantity2
			}
		}
	}

	bf := byteframe.NewByteFrame()
	bf.WriteUint16(pkt.HR)
	if _config.ErupeConfig.RealClientMode >= _config.G1 {
		bf.WriteUint16(pkt.GR)
	}
	var stamps, rewardTier, rewardUnk uint16
	reward := mhfitem.MHFItemStack{Item: mhfitem.MHFItem{}}
	s.server.db.QueryRow(`UPDATE characters SET stampcard = stampcard + $1 WHERE id = $2 RETURNING stampcard`, pkt.Stamps, s.charID).Scan(&stamps)
	bf.WriteUint16(stamps - pkt.Stamps)
	bf.WriteUint16(stamps)

	if stamps/30 > (stamps-pkt.Stamps)/30 {
		rewardTier = 2
		rewardUnk = pkt.Reward2
		reward = mhfitem.MHFItemStack{Item: mhfitem.MHFItem{ItemID: pkt.Item2}, Quantity: pkt.Quantity2}
		addWarehouseItem(s, reward)
	} else if stamps/15 > (stamps-pkt.Stamps)/15 {
		rewardTier = 1
		rewardUnk = pkt.Reward1
		reward = mhfitem.MHFItemStack{Item: mhfitem.MHFItem{ItemID: pkt.Item1}, Quantity: pkt.Quantity1}
		addWarehouseItem(s, reward)
	}

	bf.WriteUint16(rewardTier)
	bf.WriteUint16(rewardUnk)
	bf.WriteUint16(reward.Item.ItemID)
	bf.WriteUint16(reward.Quantity)
	doAckBufSucceed(s, pkt.AckHandle, bf.Data())
}

func handleMsgMhfStampcardPrize(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgMhfUnreserveSrg(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfUnreserveSrg)
	doAckSimpleSucceed(s, pkt.AckHandle, make([]byte, 4))
}

func handleMsgMhfKickExportForce(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgMhfGetEarthStatus(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfGetEarthStatus)
	bf := byteframe.NewByteFrame()
	bf.WriteUint32(uint32(TimeWeekStart().Unix())) // Start
	bf.WriteUint32(uint32(TimeWeekNext().Unix()))  // End
	bf.WriteInt32(s.server.erupeConfig.EarthStatus)
	bf.WriteInt32(s.server.erupeConfig.EarthID)
	for i, m := range s.server.erupeConfig.EarthMonsters {
		if _config.ErupeConfig.RealClientMode <= _config.G9 {
			if i == 3 {
				break
			}
		}
		if i == 4 {
			break
		}
		bf.WriteInt32(m)
	}
	doAckBufSucceed(s, pkt.AckHandle, bf.Data())
}

func handleMsgMhfRegistSpabiTime(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgMhfGetEarthValue(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfGetEarthValue)
	type EarthValues struct {
		Value []uint32
	}

	var earthValues []EarthValues
	switch pkt.ReqType {
	case 1:
		earthValues = []EarthValues{
			{[]uint32{1, 312, 0, 0, 0, 0}},
			{[]uint32{2, 99, 0, 0, 0, 0}},
		}
	case 2:
		earthValues = []EarthValues{
			{[]uint32{1, 5771, 0, 0, 0, 0}},
			{[]uint32{2, 1847, 0, 0, 0, 0}},
		}
	case 3:
		earthValues = []EarthValues{
			{[]uint32{1001, 36, 0, 0, 0, 0}},
			{[]uint32{9001, 3, 0, 0, 0, 0}},
			{[]uint32{9002, 10, 300, 0, 0, 0}},
		}
	}

	var data []*byteframe.ByteFrame
	for _, i := range earthValues {
		bf := byteframe.NewByteFrame()
		for _, j := range i.Value {
			bf.WriteUint32(j)
		}
		data = append(data, bf)
	}
	doAckEarthSucceed(s, pkt.AckHandle, data)
}

func handleMsgMhfDebugPostValue(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgMhfGetRandFromTable(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfGetRandFromTable)
	bf := byteframe.NewByteFrame()
	for i := uint16(0); i < pkt.Results; i++ {
		bf.WriteUint32(0)
	}
	doAckBufSucceed(s, pkt.AckHandle, bf.Data())
}

func handleMsgMhfGetSenyuDailyCount(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfGetSenyuDailyCount)
	bf := byteframe.NewByteFrame()
	bf.WriteUint16(0)
	bf.WriteUint16(0)
	doAckBufSucceed(s, pkt.AckHandle, bf.Data())
}

type SeibattleTimetable struct {
	Start time.Time
	End   time.Time
}

type SeibattleKeyScore struct {
	Unk0 uint8
	Unk1 int32
}

type SeibattleCareer struct {
	Unk0 uint16
	Unk1 uint16
	Unk2 uint16
}

type SeibattleOpponent struct {
	Unk0 int32
	Unk1 int8
}

type SeibattleConventionResult struct {
	Unk0 uint32
	Unk1 uint16
	Unk2 uint16
	Unk3 uint16
	Unk4 uint16
}

type SeibattleCharScore struct {
	Unk0 uint32
}

type SeibattleCurResult struct {
	Unk0 uint32
	Unk1 uint16
	Unk2 uint16
	Unk3 uint16
}

type Seibattle struct {
	Timetable        []SeibattleTimetable
	KeyScore         []SeibattleKeyScore
	Career           []SeibattleCareer
	Opponent         []SeibattleOpponent
	ConventionResult []SeibattleConventionResult
	CharScore        []SeibattleCharScore
	CurResult        []SeibattleCurResult
}

func handleMsgMhfGetSeibattle(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfGetSeibattle)
	var data []*byteframe.ByteFrame
	seibattle := Seibattle{
		Timetable: []SeibattleTimetable{
			{TimeMidnight(), TimeMidnight().Add(time.Hour * 8)},
			{TimeMidnight().Add(time.Hour * 8), TimeMidnight().Add(time.Hour * 16)},
			{TimeMidnight().Add(time.Hour * 16), TimeMidnight().Add(time.Hour * 24)},
		},
		KeyScore: []SeibattleKeyScore{
			{0, 0},
		},
		Career: []SeibattleCareer{
			{0, 0, 0},
		},
		Opponent: []SeibattleOpponent{
			{1, 1},
		},
		ConventionResult: []SeibattleConventionResult{
			{0, 0, 0, 0, 0},
		},
		CharScore: []SeibattleCharScore{
			{0},
		},
		CurResult: []SeibattleCurResult{
			{0, 0, 0, 0},
		},
	}

	switch pkt.Type {
	case 1:
		for _, timetable := range seibattle.Timetable {
			bf := byteframe.NewByteFrame()
			bf.WriteUint32(uint32(timetable.Start.Unix()))
			bf.WriteUint32(uint32(timetable.End.Unix()))
			data = append(data, bf)
		}
	case 3: // Key score?
		for _, keyScore := range seibattle.KeyScore {
			bf := byteframe.NewByteFrame()
			bf.WriteUint8(keyScore.Unk0)
			bf.WriteInt32(keyScore.Unk1)
			data = append(data, bf)
		}
	case 4: // Career?
		for _, career := range seibattle.Career {
			bf := byteframe.NewByteFrame()
			bf.WriteUint16(career.Unk0)
			bf.WriteUint16(career.Unk1)
			bf.WriteUint16(career.Unk2)
			data = append(data, bf)
		}
	case 5: // Opponent?
		for _, opponent := range seibattle.Opponent {
			bf := byteframe.NewByteFrame()
			bf.WriteInt32(opponent.Unk0)
			bf.WriteInt8(opponent.Unk1)
			data = append(data, bf)
		}
	case 6: // Convention result?
		for _, conventionResult := range seibattle.ConventionResult {
			bf := byteframe.NewByteFrame()
			bf.WriteUint32(conventionResult.Unk0)
			bf.WriteUint16(conventionResult.Unk1)
			bf.WriteUint16(conventionResult.Unk2)
			bf.WriteUint16(conventionResult.Unk3)
			bf.WriteUint16(conventionResult.Unk4)
			data = append(data, bf)
		}
	case 7: // Char score?
		for _, charScore := range seibattle.CharScore {
			bf := byteframe.NewByteFrame()
			bf.WriteUint32(charScore.Unk0)
			data = append(data, bf)
		}
	case 8: // Cur result?
		for _, curResult := range seibattle.CurResult {
			bf := byteframe.NewByteFrame()
			bf.WriteUint32(curResult.Unk0)
			bf.WriteUint16(curResult.Unk1)
			bf.WriteUint16(curResult.Unk2)
			bf.WriteUint16(curResult.Unk3)
			data = append(data, bf)
		}
	}
	doAckEarthSucceed(s, pkt.AckHandle, data)
}

func handleMsgMhfPostSeibattle(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfPostSeibattle)
	doAckSimpleSucceed(s, pkt.AckHandle, make([]byte, 4))
}

func handleMsgMhfGetDailyMissionMaster(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgMhfGetDailyMissionPersonal(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgMhfSetDailyMissionPersonal(s *Session, p mhfpacket.MHFPacket) {}

func equipSkinHistSize() int {
	size := 3200
	if _config.ErupeConfig.RealClientMode <= _config.Z2 {
		size = 2560
	}
	if _config.ErupeConfig.RealClientMode <= _config.Z1 {
		size = 1280
	}
	return size
}

func handleMsgMhfGetEquipSkinHist(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfGetEquipSkinHist)
	size := equipSkinHistSize()
	var data []byte
	err := s.server.db.QueryRow("SELECT COALESCE(skin_hist::bytea, $2::bytea) FROM characters WHERE id = $1", s.charID, make([]byte, size)).Scan(&data)
	if err != nil {
		s.logger.Error("Failed to load skin_hist", zap.Error(err))
		data = make([]byte, size)
	}
	doAckBufSucceed(s, pkt.AckHandle, data)
}

func handleMsgMhfUpdateEquipSkinHist(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfUpdateEquipSkinHist)
	size := equipSkinHistSize()
	var data []byte
	err := s.server.db.QueryRow("SELECT COALESCE(skin_hist, $2) FROM characters WHERE id = $1", s.charID, make([]byte, size)).Scan(&data)
	if err != nil {
		s.logger.Error("Failed to get skin_hist", zap.Error(err))
		doAckSimpleSucceed(s, pkt.AckHandle, make([]byte, 4))
		return
	}

	bit := int(pkt.ArmourID) - 10000
	startByte := (size / 5) * int(pkt.MogType)
	// psql set_bit could also work but I couldn't get it working
	byteInd := bit / 8
	bitInByte := bit % 8
	data[startByte+byteInd] |= bits.Reverse8(1 << uint(bitInByte))
	dumpSaveData(s, data, "skinhist")
	s.server.db.Exec("UPDATE characters SET skin_hist=$1 WHERE id=$2", data, s.charID)
	doAckSimpleSucceed(s, pkt.AckHandle, make([]byte, 4))
}

func handleMsgMhfGetUdShopCoin(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfGetUdShopCoin)
	bf := byteframe.NewByteFrame()
	bf.WriteUint32(0)
	doAckSimpleSucceed(s, pkt.AckHandle, bf.Data())
}

func handleMsgMhfUseUdShopCoin(s *Session, p mhfpacket.MHFPacket) {}

func handleMsgMhfGetEnhancedMinidata(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfGetEnhancedMinidata)
	// this looks to be the detailed chunk of information you can pull up on players in town
	var data []byte
	err := s.server.db.QueryRow("SELECT minidata FROM characters WHERE id = $1", pkt.CharID).Scan(&data)
	if err != nil {
		s.logger.Error("Failed to load minidata")
		data = make([]byte, 1)
	}
	doAckBufSucceed(s, pkt.AckHandle, data)
}

func handleMsgMhfSetEnhancedMinidata(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfSetEnhancedMinidata)
	dumpSaveData(s, pkt.RawDataPayload, "minidata")
	_, err := s.server.db.Exec("UPDATE characters SET minidata=$1 WHERE id=$2", pkt.RawDataPayload, s.charID)
	if err != nil {
		s.logger.Error("Failed to save minidata", zap.Error(err))
	}
	doAckSimpleSucceed(s, pkt.AckHandle, []byte{0x00, 0x00, 0x00, 0x00})
}

func handleMsgMhfGetLobbyCrowd(s *Session, p mhfpacket.MHFPacket) {
	// this requests a specific server's population but seems to have been
	// broken at some point on live as every example response across multiple
	// servers sends back the exact same information?
	// It can be worried about later if we ever get to the point where there are
	// full servers to actually need to migrate people from and empty ones to
	pkt := p.(*mhfpacket.MsgMhfGetLobbyCrowd)
	doAckBufSucceed(s, pkt.AckHandle, make([]byte, 0x320))
}

type TrendWeapon struct {
	WeaponType uint8
	WeaponID   uint16
}

func handleMsgMhfGetTrendWeapon(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfGetTrendWeapon)
	trendWeapons := [14][3]TrendWeapon{}
	for i := uint8(0); i < 14; i++ {
		rows, err := s.server.db.Query(`SELECT weapon_id FROM trend_weapons WHERE weapon_type=$1 ORDER BY count DESC LIMIT 3`, i)
		if err != nil {
			continue
		}
		j := 0
		for rows.Next() {
			trendWeapons[i][j].WeaponType = i
			rows.Scan(&trendWeapons[i][j].WeaponID)
			j++
		}
	}

	x := uint8(0)
	bf := byteframe.NewByteFrame()
	bf.WriteUint8(0)
	for _, weaponType := range trendWeapons {
		for _, weapon := range weaponType {
			bf.WriteUint8(weapon.WeaponType)
			bf.WriteUint16(weapon.WeaponID)
			x++
		}
	}
	bf.Seek(0, 0)
	bf.WriteUint8(x)
	doAckBufSucceed(s, pkt.AckHandle, bf.Data())
}

func handleMsgMhfUpdateUseTrendWeaponLog(s *Session, p mhfpacket.MHFPacket) {
	pkt := p.(*mhfpacket.MsgMhfUpdateUseTrendWeaponLog)
	s.server.db.Exec(`INSERT INTO trend_weapons (weapon_id, weapon_type, count) VALUES ($1, $2, 1) ON CONFLICT (weapon_id) DO
		UPDATE SET count = trend_weapons.count+1`, pkt.WeaponID, pkt.WeaponType)
	doAckSimpleSucceed(s, pkt.AckHandle, make([]byte, 4))
}
