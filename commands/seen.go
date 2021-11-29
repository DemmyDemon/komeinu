package commands

import (
	"fmt"
	"komainu/storage"
	"log"
	"strings"
	"time"

	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
)

func CommandSeen(state *state.State, event *gateway.InteractionCreateEvent, command *discord.CommandInteraction) api.InteractionResponse {
	if command.Options != nil && len(command.Options) > 0 {
		option, err := command.Options[0].SnowflakeValue()
		if err != nil {
			log.Printf("[%s] Failed to get snowflake value for /seen: %s\n", event.GuildID, err)
			return ResponseMessage("An error occured, and has been logged.")
		}
		if me, err := state.Me(); err != nil {
			log.Printf("[%s] Failed to look up myself to see if I match /seen: %s\n", event.GuildID, err)
			return ResponseMessage("An error occured, and has been logged.")
		} else if me.ID == discord.UserID(option) {
			return ResponseMessage("I'm right here, buddy!")
		}
		if found, timestamp, err := storage.LastSeen(event.GuildID, discord.UserID(option)); err != nil {
			log.Printf("[%s] Failed to get %s from Sniper for /seen lookup: %s\n", event.GuildID, option, err)
			return ResponseMessage("An error occured, and has been logged.")
		} else if !found {
			return ResponseMessageNoMention(fmt.Sprintf("Sorry, I've never seen <@%s> say anything at all!", option))
		} else {
			return ResponseMessageNoMention(fmt.Sprintf("I last saw <@%s> <t:%d:R>", option, timestamp))
		}
	}
	return ResponseMessage("No user given?!")
}

func CommandInactive(state *state.State, event *gateway.InteractionCreateEvent, command *discord.CommandInteraction) api.InteractionResponse {
	days := int64(30)
	if command.Options != nil && len(command.Options) > 0 {
		d, err := command.Options[0].IntValue()
		if err != nil {
			log.Printf("[%s] Failed to get int value for /inactive: %s", event.GuildID, err)
			return ResponseMessage("An error occured, and has been logged.")
		}
		if d <= 0 {
			return ResponseMessage(fmt.Sprintf("Everyone. Everyone has been inactive for %d days.", d))
		}
		days = d
	}
	atLeast := time.Now().Unix() - (24 * 3600 * days)
	members, err := state.Session.Members(event.GuildID, 0)
	if err != nil {
		log.Printf("[%s] Failed to get member list for /inactive lookup: %s", event.GuildID, err)
		return ResponseMessage("An error occured, and has been logged.")
	}

	never := []discord.UserID{}
	var sb strings.Builder

	inactiveCount := 0

	for _, member := range members {
		seen, when, err := storage.LastSeen(event.GuildID, member.User.ID)
		if err != nil {
			log.Printf("[%s] Failed to get a storage.LastSeen for %s: %s", event.GuildID, member.User.ID, err)
			return ResponseMessage("An error occured, and has been logged.")
		} else if !seen {
			never = append(never, member.User.ID)
		} else if when <= atLeast {
			fmt.Fprintf(&sb, "<@%d> <t:%d:R>\n", member.User.ID, when)
			inactiveCount++
		}
	}
	fmt.Fprintf(&sb, "%d inactive in the last %d days, out of %d members.\n", inactiveCount+len(never), days, len(members))
	if len(never) > 0 {
		fmt.Fprint(&sb, "Never seen active by me: ")
		for _, userID := range never {
			fmt.Fprintf(&sb, "<@%d> ", userID)
		}
	}
	return ResponseMessageNoMention(sb.String())
}
