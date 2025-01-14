package discordbot

import (
	_config "erupe-ce/config"
	"regexp"

	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"
)

var Commands = []*discordgo.ApplicationCommand{
	{
		Name:        "link",
		Description: "Link your Erupe account to Discord",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "token",
				Description: "The token provided by the Discord command in-game",
				Required:    true,
			},
		},
	},
	{
		Name:        "password",
		Description: "Change your Erupe account password",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "password",
				Description: "Your new password",
				Required:    true,
			},
		},
	},
}

type DiscordBot struct {
	Session      *discordgo.Session
	config       *_config.Config
	logger       *zap.Logger
	MainGuild    *discordgo.Guild
	RelayChannel *discordgo.Channel
}

type Options struct {
	Config *_config.Config
	Logger *zap.Logger
}

func NewDiscordBot(options Options) (discordBot *DiscordBot, err error) {
	session, err := discordgo.New("Bot " + options.Config.Discord.BotToken)

	if err != nil {
		options.Logger.Fatal("Discord failed", zap.Error(err))
		return nil, err
	}

	var relayChannel *discordgo.Channel

	if options.Config.Discord.RelayChannel.Enabled {
		relayChannel, err = session.Channel(options.Config.Discord.RelayChannel.RelayChannelID)
	}

	if err != nil {
		options.Logger.Fatal("Discord failed to create relayChannel", zap.Error(err))
		return nil, err
	}

	discordBot = &DiscordBot{
		config:       options.Config,
		logger:       options.Logger,
		Session:      session,
		RelayChannel: relayChannel,
	}

	return
}

func (bot *DiscordBot) Start() (err error) {
	err = bot.Session.Open()

	return
}

// NormalizeDiscordMessage replaces all mentions to real name from the message.
func (bot *DiscordBot) NormalizeDiscordMessage(message string) string {
	userRegex := regexp.MustCompile(`<@!?(\d{17,19})>`)
	emojiRegex := regexp.MustCompile(`(?:<a?)?:(\w+):(?:\d{18}>)?`)
	messageRegex := regexp.MustCompile(`[^\x{20}-\x{7E}]`)

	result := ReplaceTextAll(message, userRegex, func(userId string) string {
		user, err := bot.Session.User(userId)

		if err != nil {
			return "@unknown" // @Unknown
		}

		return "@" + user.Username
	})

	result = ReplaceTextAll(result, emojiRegex, func(emojiName string) string {
		return ":" + emojiName + ":"
	})

	result = messageRegex.ReplaceAllString(result, "")

	return result
}

func (bot *DiscordBot) RealtimeChannelSend(message string) (err error) {
	if bot.RelayChannel == nil {
		return
	}

	_, err = bot.Session.ChannelMessageSend(bot.RelayChannel.ID, message)

	return
}
func ReplaceTextAll(text string, regex *regexp.Regexp, handler func(input string) string) string {
	result := regex.ReplaceAllFunc([]byte(text), func(s []byte) []byte {
		input := regex.ReplaceAllString(string(s), `$1`)

		return []byte(handler(input))
	})

	return string(result)
}
