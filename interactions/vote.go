package interactions

import (
	"errors"
	"fmt"
	"komainu/interactions/command"
	"komainu/interactions/component"
	"komainu/interactions/delete"
	"komainu/interactions/modal"
	"komainu/interactions/response"
	"komainu/storage"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/diamondburned/arikawa/v3/utils/json/option"
)

func init() {
	command.Register("vote", commandVoteObject)
	component.Register("vote", component.Handler{Code: ComponentVote})
	delete.Register(delete.Handler{Code: DeleteVote})
	modal.Register("votestart", modal.Handler{Code: VoteModalHandler})
}

var commandVoteObject = command.Handler{
	Description: "Initiate a vote",
	Code:        CommandVote,
	Options: []discord.CommandOption{
		&discord.NumberOption{
			OptionName:  "length",
			Description: "The number of days the vote should run.",
			Required:    true,
			Min:         option.NewFloat(0),
			Max:         option.NewFloat(365),
		},
	},
}

// DeleteVote will delete the appropriate vote when the message it's in is deleted.
func DeleteVote(state *state.State, kvs storage.KeyValueStore, e *gateway.MessageDeleteEvent) {
	if e.GuildID == discord.NullGuildID {
		return
	}
	err := kvs.Delete(e.GuildID, "votes", e.ID)
	if err != nil {
		log.Printf("[%s] Encountered an error removing vote from KVS after message deletion: %s\n", e.GuildID, err)
	}
}

// ComponentVote attempts to handle the given interaction as a vote
func ComponentVote(state *state.State, kvs storage.KeyValueStore, e *gateway.InteractionCreateEvent, interaction discord.ComponentInteraction) api.InteractionResponse {
	isVote, resp, err := handleInteractionAsVote(state, kvs, e, interaction)
	if err != nil {
		log.Printf("[%s] error while trying to handle an interaction as a vote: %s\n", e.GuildID, err)
		return response.Ephemeral("Something went wrong. It was logged, so hopefully it'll get fixed.")
	}
	if isVote && resp != "" {
		return response.Ephemeral(resp)
	}
	log.Printf("[%s] Empty response or non-vote submitted as vote interaction!", e.GuildID)
	return response.Ephemeral("I'm sorry, but I can't find the poll you are trying to vote on?!")
}

// CommandVote processes a command to start a vote
func CommandVote(state *state.State, kvs storage.KeyValueStore, event *gateway.InteractionCreateEvent, cmd *discord.CommandInteraction) command.Response {
	if cmd.Options != nil && len(cmd.Options) > 1 {
		log.Printf("[%s] /vote command structure is somehow nil or not the correct number of elements. Wat.\n", event.GuildID)
		return command.Response{Response: response.Ephemeral("Yeah, no, that didn't work."), Callback: nil}
	}

	days, err := cmd.Options[0].FloatValue()
	if err != nil {
		log.Printf("[%s] /vote command structure is somehow weird. Could not get the Float value of the days option.\n", event.GuildID)
		return command.Response{Response: response.Ephemeral("Wait, what? How many hours? Try again."), Callback: nil}
	}

	form := []discord.TextInputComponent{
		{
			CustomID:     discord.ComponentID(fmt.Sprintf("desc/%f", days)),
			Style:        discord.TextInputParagraphStyle,
			Label:        "Description of the vote",
			LengthLimits: [2]int{1, 500},
			Value:        option.NewNullableString(""),
			Placeholder:  option.NewNullableString("Describe what everyone is supposed to be voting about."),
		},
		{
			CustomID:    discord.ComponentID("options"),
			Style:       discord.TextInputParagraphStyle,
			Label:       "Options, 1/line, max 25, max 100 chars/line",
			Value:       option.NewNullableString("Yes\nNo"),
			Placeholder: &option.NullableStringData{},
		},
	}

	return command.Response{Response: modal.Respond(
		event.SenderID(), event.GuildID, "votestart", "Call a vote!", form...,
	), Callback: nil}
}

func VoteModalHandler(state *state.State, kvs storage.KeyValueStore, event *gateway.InteractionCreateEvent, interaction *discord.ModalInteraction) command.Response {
	vote := storage.Vote{
		StartTime: time.Now().Unix(),
		EndTime:   0,
		GuildID:   event.GuildID,
		MessageID: discord.NullMessageID, // This is added in the MessageID callback later.
		ChannelID: discord.NullChannelID, // This one, too!
		Question:  "",
		Options:   map[string]string{},
		Order:     []string{},
		Votes:     map[discord.UserID]string{},
	}
	data := modal.DecodeModalResponse(interaction.Components)
	for key, value := range data {
		if strings.HasPrefix(key, "desc/") {
			if vote.Question != "" {
				log.Printf("[%s] Duplicate Question in vote configuration.", event.GuildID)
				return command.Response{Response: response.Ephemeral("There was a problem processing your vote configuration. It has been logged.")}
			}
			vote.Question = value
			days, err := strconv.ParseFloat(strings.TrimPrefix(key, "desc/"), 64)
			if err != nil {
				log.Printf("[%s] Error processing vote length: %s", event.GuildID, err)
				return command.Response{Response: response.Ephemeral("There was an error processing your vote configuration. It has been logged.")}
			}
			vote.EndTime = vote.StartTime + int64(days*24*float64(3600)) // 24 hours per day, 3600 seconds per hour
		} else if key == "options" {
			optionList := strings.Split(value, "\n")
			for i, opt := range optionList {
				if i > 24 {
					break
				}
				if len(opt) > 100 {
					opt = opt[0:100]
				}
				item := "vote/" + strconv.Itoa(i)
				vote.Options[item] = opt
				vote.Order = append(vote.Order, item)
			}
		} else {
			log.Printf("[%s] Unknown prefix while processing vote modal: %s", event.GuildID, key)
			return command.Response{Response: response.Ephemeral("Something strange happened while processing your vote configuration. It has been logged.")}
		}
	}

	return command.Response{
		Response: api.InteractionResponse{
			Type: api.MessageInteractionWithSource,
			Data: &api.InteractionResponseData{
				Content:    option.NewNullableString(vote.String()),
				Components: makeVoteSelector(&vote),
			},
		},
		Callback: func(message *discord.Message) {
			vote.MessageID = message.ID
			vote.ChannelID = message.ChannelID
			err := vote.Store(kvs)
			if err != nil {
				log.Printf("[%s] Failed to save vote afer adding MessageID (%s) and ChannelID (%s)", vote.GuildID, message.ID, message.ChannelID)
			}
		},
	}
}

func makeVoteSelector(vote *storage.Vote) *discord.ContainerComponents {
	var selectable []discord.SelectOption
	for key, label := range vote.Options {
		selectable = append(selectable, discord.SelectOption{
			Label: label,
			Value: key,
		})
	}
	row := discord.ActionRowComponent([]discord.InteractiveComponent{
		&discord.SelectComponent{
			Options:     selectable,
			CustomID:    "vote",
			Placeholder: "Cast your vote!",
			ValueLimits: [2]int{0, 1},
		},
	})
	return discord.ComponentsPtr(&row)
}

// handleInteractionAsVote determines if the given interaction is a vote button click, and acts accordingly.
func handleInteractionAsVote(state *state.State, kvs storage.KeyValueStore, e *gateway.InteractionCreateEvent, interaction discord.ComponentInteraction) (isVote bool, response string, err error) {
	exist, vote, err := storage.GetVote(kvs, e.GuildID, e.Message.ID)
	if err != nil {
		return true, "Something very odd happened.", fmt.Errorf("handling interaction as vote: %w", err)
	}
	if !exist {
		return false, "", nil
	}

	now := time.Now().Unix()
	if vote.EndTime <= now {
		return true, "I'm sorry, that vote is closed!", nil
	}

	selector, ok := interaction.(*discord.SelectInteraction)

	if !ok {
		return true, "Your response was not in the right format, somehow?!", errors.New("submitted vote was not from a SelectInteraction")
	}

	if len(selector.Values) != 1 {
		return true, "You must select exactly one item", fmt.Errorf("%d values selected in vote, expected 1", len(selector.Values))
	}

	voted := selector.Values[0]

	label, ok := vote.Options[voted]
	if !ok {
		return true, "Sorry, you can't vote for that.", fmt.Errorf("vote cast for %s, which is not an option", voted)
	}

	vote.Votes[e.SenderID()] = voted
	if _, err := state.EditMessage(e.ChannelID, e.Message.ID, vote.String()); err != nil {
		return true, "There was an error registering your vote.", fmt.Errorf("handling interaction as vote: %w", err)
	}
	if err := vote.Store(kvs); err != nil {
		return true, "There was an error storing your vote.", fmt.Errorf("storing a vote: %w", err)
	}
	return true, fmt.Sprintf("Your vote for...\n%s\n...is registered.", label), nil
}
