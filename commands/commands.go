package commands

import (
	"komainu/storage"
	"log"
	"strings"

	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/diamondburned/arikawa/v3/utils/json/option"
)

type CommandFunction func(
	state *state.State,
	sniper storage.KeyValueStore,
	event *gateway.InteractionCreateEvent,
	command *discord.CommandInteraction,
) api.InteractionResponse

type Command struct {
	group       string
	description string
	code        CommandFunction
	options     []discord.CommandOption
}

var commands = map[string]Command{
	"access": {"access", "Grant, revoke and list command group access", CommandAccess, []discord.CommandOption{
		&discord.SubcommandOption{
			OptionName:  "grant",
			Description: "Grant a role access to something",
			Options: []discord.CommandOptionValue{
				&discord.StringOption{
					OptionName:  "group",
					Description: "The command group to grant access to",
					Required:    true,
				},
				&discord.RoleOption{
					OptionName:  "role",
					Description: "The role that gets this access",
					Required:    true,
				},
			},
		},
		&discord.SubcommandOption{
			OptionName:  "revoke",
			Description: "Revoke access to something from a role",
			Options: []discord.CommandOptionValue{
				&discord.StringOption{
					OptionName:  "group",
					Description: "The command group to revoke access from",
					Required:    true,
				},
				&discord.RoleOption{
					OptionName:  "role",
					Description: "The role that loses this access",
					Required:    true,
				},
			},
		},
		&discord.SubcommandOption{
			OptionName:  "list",
			Description: "List what roles have access to what command groups",
			Options:     []discord.CommandOptionValue{},
		},
	}},

	"seen": {"seen", "Check when someone was last around", CommandSeen, []discord.CommandOption{
		&discord.UserOption{
			OptionName:  "user",
			Description: "The user to look up",
			Required:    true,
		},
	}},
	"inactive": {"seen", "Get a list of inactive people", CommandInactive, []discord.CommandOption{
		&discord.IntegerOption{
			OptionName:  "days",
			Description: "How many days of quiet makes someone inactive?",
			Required:    true,
		},
	}},

	"faq": {"faquser", "Look up a FAQ topic", CommandFaq, []discord.CommandOption{
		&discord.StringOption{
			OptionName:  "topic",
			Description: "The name of the topic you wish to recall",
			Required:    true,
		},
	}},
	"faqset": {"faqadmin", "Manage FAQ topics", CommandFaqSet, []discord.CommandOption{
		&discord.SubcommandOption{
			OptionName:  "add",
			Description: "Add a topic to the FAQ",
			Options: []discord.CommandOptionValue{
				&discord.StringOption{
					OptionName:  "topic",
					Description: "The word used to recall this item later",
					Required:    true,
				},
				&discord.StringOption{
					OptionName:  "content",
					Description: "What you want the topic to contain",
					Required:    true,
				},
			},
		},
		&discord.SubcommandOption{
			OptionName:  "remove",
			Description: "Remove a topic from the FAQ",
			Options: []discord.CommandOptionValue{
				&discord.StringOption{
					OptionName:  "topic",
					Description: "What do you want to permanently obliterate from the FAQ?",
					Required:    true,
				},
			},
		},
		&discord.SubcommandOption{
			OptionName:  "list",
			Description: "List the known topics in the FAQ",
			Options:     []discord.CommandOptionValue{},
		},
	}},

	// "vote": {"vote", "Initiate a vote", CommandVote, []discord.CommandOption{}},
}

// HasAccess checks if the given user has access to the given command group in the given guild.
func HasAccess(sniper storage.KeyValueStore, state *state.State, guildID discord.GuildID, channelID discord.ChannelID, member *discord.Member, group string) bool {
	if member == nil {
		return false
	}

	// TODO: Check member.RoleIDs against the roles stored under group string in Sniper

	if guild, err := state.Guild(guildID); err != nil {
		log.Printf("Could not look up guild %s for access check: %s\n", guildID, err)
		return false // Better safe than sorry!
	} else if guild.OwnerID == member.User.ID {
		return true // Owner always has access to everything.
	}

	if permissions, err := state.Permissions(channelID, member.User.ID); err != nil {
		log.Printf("Could not look up permissions for %s in channel %s for access check: %s\n", member.User.ID, channelID, err)
		return false // Better safe than sorry!
	} else if permissions.Has(discord.PermissionAdministrator) {
		return true // Administrators get access to everyting
	}

	return false // If all else fails, they're not authorized.
}

// AddCommandHandler, surprisingly, adds the command handler.
func AddCommandHandler(state *state.State, sniper storage.KeyValueStore) {
	state.AddHandler(func(e *gateway.InteractionCreateEvent) {
		command, ok := e.Data.(*discord.CommandInteraction)
		if !ok {
			return
		}
		if val, ok := commands[command.Name]; ok {
			if !HasAccess(sniper, state, e.GuildID, e.ChannelID, e.Member, val.group) {
				if err := state.RespondInteraction(e.ID, e.Token, ResponseMessage("Sorry, access was denied.")); err != nil {
					log.Println("An error occured posting access denied response:", err)
				}
				return
			}

			response := val.code(state, sniper, e, command)

			if err := state.RespondInteraction(e.ID, e.Token, response); err != nil {
				log.Println("Failed to send interaction resposne:", err)
			}
		}
	})
}

// RegisterCommands registers the command in the given guild, clearing out any obsolete commands.
func RegisterCommands(state *state.State, guildID discord.GuildID) {
	app, err := state.CurrentApplication()
	if err != nil {
		log.Println("Failed to register commands: Could not determine app ID:", err)
		return
	}

	currentCommands, err := state.GuildCommands(app.ID, guildID)
	if err != nil {
		log.Printf("[%s] Failed to register commands: Could not determine current guild commands:%s\n", guildID, err)
		return
	}
	for _, command := range currentCommands {
		if command.AppID == app.ID {
			if _, ok := commands[command.Name]; !ok {
				if err := state.DeleteGuildCommand(app.ID, guildID, command.ID); err != nil {
					log.Printf("[%s] Tried to remove obsolete command /%s, but %s\n", guildID, command.Name, err)
				}
			}
		}
	}

	for name, data := range commands {
		_, err := state.CreateGuildCommand(app.ID, guildID, api.CreateCommandData{
			Name:        name,
			Description: data.description,
			Options:     data.options,
		})
		if err != nil {
			log.Printf("[%s] Failed to create guild command /%s: %s\n", guildID, name, err)
		} else {
			log.Printf("[%s] Registered command /%s", guildID, name)
		}
	}
}

// ResponseMessage generates an InteractionResponse from the strings given.
func ResponseMessage(message ...string) api.InteractionResponse {
	return api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &api.InteractionResponseData{
			Content: option.NewNullableString(strings.Join(message, " ")),
		},
	}
}

// ResponseMessageNoMention generates an InteractionResponse from the strings given, and suppresses any mentions this might cause.
func ResponseMessageNoMention(message ...string) api.InteractionResponse {
	return api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &api.InteractionResponseData{
			Content: option.NewNullableString(strings.Join(message, " ")),
			AllowedMentions: &api.AllowedMentions{
				Parse: []api.AllowedMentionType{},
			},
		},
	}
}
