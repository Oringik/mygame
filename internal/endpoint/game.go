package endpoint

import (
	"context"
	"encoding/json"
	"log"
	"mygame/config"
	"mygame/internal/singleton"
	"mygame/tools/jwt"
	"os"
	"time"
)

type EventType string

const (
	StartGame     EventType = "start_game"
	Join          EventType = "join"
	Disconnect    EventType = "disconnect"
	GetQuest      EventType = "get_quest"
	ChooseQuest   EventType = "choose_quest"
	GiveAnswer    EventType = "give_answer"
	DeclineAnswer EventType = "decline_answer"
	AcceptAnswer  EventType = "accept_answer"
)

var roleByEvent = map[EventType][]Role{
	StartGame:     {Leader},
	Join:          {},
	Disconnect:    {},
	GetQuest:      {User},
	GiveAnswer:    {User},
	DeclineAnswer: {Leader},
	AcceptAnswer:  {Leader},
	ChooseQuest:   {User},
}

type ServerEventType string

const (
	GreetingsServer      ServerEventType = "greetings_server"
	ReadingRoundServer   ServerEventType = "reading_round"
	ReadingThemesServer  ServerEventType = "reading_themes_server"
	WallServer           ServerEventType = "wall_server"
	GetQuestServer       ServerEventType = "get_quest_server"
	JoinServer           ServerEventType = "join_server"
	DisconnectServer     ServerEventType = "disconnect_server"
	ChooseQuestServer    ServerEventType = "choose_quest_server"
	TakenQuestServer     ServerEventType = "taken_quest_server"
	ScoreChangedServer   ServerEventType = "score_changed"
	AnswerAcceptedServer ServerEventType = "answer_accepted_server"
	AnswerDeclinedServer ServerEventType = "answer_declined_server"
	FinalServer          ServerEventType = "final_server"
)

type ClientEvent struct {
	Type  EventType
	Token string
	Data  json.RawMessage
}

type ChooseQuestClientEvent struct {
	ThemeID    int
	QuestionID int
}

type Step int

const (
	WaitingStart Step = iota
	Grettings
	ReadingRound
	ReadingThemes
	ChooseQuestion
	Getting
	Answering
	Pause
	Final
)

type ServerEvent struct {
	Type ServerEventType
	Exp  int64
	Data interface{}
}

type JoinServerEvent struct {
	QueueID  int
	Nickname string
	ImageUID string
}

type DisconnectServerEvent struct {
	QueueID int
}

type GreetingsServerEvent struct {
	Name   string
	Author string
	Date   string
}

type ReadingRoundServerEvent struct {
	Name string
}

type ReadingThemesServerEvent struct {
	ThemeNames []string
}

type WallServerEvent struct {
	Themes []*Theme
}

type ChooseQuestServerEvent struct {
	ThemeID    int
	QuestionID int
}

type TakenQuestServerEvent struct {
	QueueID int
}

type GetQuestServerEvent struct {
	QueueID int
}

type ScoreChangedServerEvent struct {
	QueueID int
	Score   int
}

type FinalServerEvent struct {
	WinnerID int
}

type Game struct {
	UID [32]byte `json:"uid"`

	Name   string   `json:"name"`
	Author string   `json:"author"`
	Date   string   `json:"date"`
	Rounds []*Round `json:"rounds"`

	hub                   *Hub
	players               map[*Client]*Player
	playersQueueIDByToken map[string]int
	playersTokenByQueueID map[int]string

	eventChannel chan *ClientEvent

	currentStep     Step
	currentPlayerID int

	currentRound    int
	currentTheme    int
	currentQuestion int

	configuration *config.Config
}

type Player struct {
	client *Client
	score  int
}

type Round struct {
	Id     int      `json:"id"`
	Name   string   `json:"name"`
	Themes []*Theme `json:"themes"`
}

type Theme struct {
	Id     int         `json:"id"`
	Name   string      `json:"name"`
	Quests []*Question `json:"quests"`
}

type ObjectType string

const (
	Text       ObjectType = "text"
	Image      ObjectType = "image"
	Audio      ObjectType = "voice"
	Video      ObjectType = "video"
	Auction    ObjectType = "auction"
	Answer     ObjectType = "answer"
	FinalRound ObjectType = "final"
	Marker     ObjectType = "marker"
)

func (o ObjectType) String() string {
	return string(o)
}

type Question struct {
	Id     int       `json:"id"`
	Price  int       `json:"price"`
	Scene  []*Object `json:"scenes"`
	Answer []*Object `json:"answers"`
}

type Object struct {
	Id   int        `json:"id"`
	Type ObjectType `json:"question_type"`
	Src  string     `json:"src"`
}

func (game *Game) runGame(ctx context.Context) {
	game.currentStep = WaitingStart
	ticker := time.NewTicker(20 * time.Minute)

	defer ticker.Stop()

	for {
		select {
		case event := <-game.eventChannel:
			token, err := jwt.ParseJWT([]byte(game.configuration.JWT.SecretKey), event.Token)
			if err != nil {
				client, ok := game.hub.clients[event.Token]
				if !ok {
					client.send <- []byte("token parse error " + err.Error())
					client.conn.Close()

					game.hub.unregister <- client
				}

				return
			}

			if token.ExpiresAt < time.Now().Unix() {
				client, ok := game.hub.clients[event.Token]
				if !ok {
					client.send <- []byte("token expired " + err.Error())
					client.conn.Close()

					game.hub.unregister <- client
				}

				return
			}

			accessedRoles := roleByEvent[event.Type]
			if len(accessedRoles) != 0 {
				var accessed bool
				for _, role := range accessedRoles {
					if role == game.hub.clients[event.Token].role {
						accessed = true
					}
				}

				if !accessed {
					game.hub.clients[event.Token].send <- []byte("permission denied")

					continue
				}
			}

			var newDuration time.Duration

			switch event.Type {
			case StartGame:
				if len(game.players) == 0 {
					game.hub.clients[event.Token].send <- []byte("cannot start game: no players")

					continue
				}

				game.currentStep = Grettings

				newDuration = 10 * time.Second

				greetingsServer := GreetingsServerEvent{
					Name:   game.Name,
					Author: game.Author,
					Date:   game.Date,
				}

				game.broadcastServerEvent(GreetingsServer, greetingsServer, time.Now().In(time.UTC).Add(newDuration).Unix())
			case Join:
				// todo: getting user image
				joinServer := JoinServerEvent{
					QueueID:  0,
					Nickname: token.Login,
				}

				if game.hub.clients[event.Token].role == Leader {
					game.broadcastServerEvent(JoinServer, joinServer, 0)

					continue
				}

				game.players[game.hub.clients[event.Token]] = &Player{
					client: game.hub.clients[event.Token],
					score:  0,
				}

				queueID := len(game.playersQueueIDByToken) + 1

				game.playersQueueIDByToken[event.Token] = queueID
				game.playersTokenByQueueID[queueID] = event.Token

				joinServer.QueueID = queueID

				game.broadcastServerEvent(JoinServer, joinServer, 0)
			case Disconnect:
				for client := range game.players {
					if client.token == event.Token {
						delete(game.players, client)
					}
				}

				disconnectServer := DisconnectServerEvent{
					QueueID: game.playersQueueIDByToken[event.Token],
				}

				game.broadcastServerEvent(DisconnectServer, disconnectServer, 0)
			case ChooseQuest:
				var clientEvent ChooseQuestClientEvent

				err = json.Unmarshal(event.Data, &clientEvent)
				if err != nil {
					log.Println(err)
					continue
				}

				game.currentTheme = clientEvent.ThemeID
				game.currentQuestion = clientEvent.QuestionID

				game.currentStep = Getting
				newDuration = 10 * time.Second

				chooseQuest := ChooseQuestServerEvent{
					ThemeID:    clientEvent.ThemeID,
					QuestionID: clientEvent.QuestionID,
				}

				// todo: send correct answer to leader

				game.broadcastServerEvent(ChooseQuestServer, chooseQuest, time.Now().In(time.UTC).Add(newDuration).Unix())
			case GetQuest:
				if game.currentStep == Getting {
					player := game.players[game.hub.clients[event.Token]]
					if player.client.role == User {
						game.currentStep = Answering
						game.currentPlayerID = game.playersQueueIDByToken[event.Token]

						newDuration = 20 * time.Second

						takenQuest := TakenQuestServerEvent{
							QueueID: game.playersQueueIDByToken[event.Token],
						}

						game.broadcastServerEvent(TakenQuestServer, takenQuest, time.Now().In(time.UTC).Add(newDuration).Unix())
					}
				}
			case AcceptAnswer:
				var found bool
				for _, theme := range game.Rounds[game.currentRound-1].Themes {
					for _, question := range theme.Quests {
						if question.Price >= 0 && question.Id != game.currentQuestion {
							found = true
						}
					}
				}
				if !found {
					if len(game.Rounds) > game.currentRound-1 {
						game.currentRound++
						game.currentStep = ChooseQuestion

						newDuration = 30 * time.Second
					} else {
						game.currentStep = Final
						newDuration = 5 * time.Minute

						var winnerID int
						var maxScore int
						for _, player := range game.players {
							if player.score > maxScore {
								maxScore = player.score
								winnerID = game.playersQueueIDByToken[player.client.token]
							}
						}

						game.broadcastServerEvent(FinalServer, FinalServerEvent{WinnerID: winnerID}, time.Now().In(time.UTC).Add(newDuration).Unix())
					}
				} else {
					game.currentStep = ChooseQuestion
					newDuration = 30 * time.Second
				}

				curQuest := game.Rounds[game.currentRound-1].Themes[game.currentTheme-1].Quests[game.currentQuestion-1]
				game.players[game.hub.clients[game.playersTokenByQueueID[game.currentPlayerID]]].score += curQuest.Price

				if len(game.players) > game.currentPlayerID {
					game.currentPlayerID++
				} else {
					game.currentPlayerID = 1
				}

				scoreChanged := ScoreChangedServerEvent{
					QueueID: game.currentPlayerID,
					Score:   game.players[game.hub.clients[event.Token]].score,
				}

				game.broadcastServerEvent(AnswerAcceptedServer, nil, time.Now().In(time.UTC).Add(newDuration).Unix())
				game.broadcastServerEvent(ScoreChangedServer, scoreChanged, 0)

			case DeclineAnswer:
				var found bool
				for _, theme := range game.Rounds[game.currentRound-1].Themes {
					for _, question := range theme.Quests {
						if question.Price >= 0 && question.Id != game.currentQuestion {
							found = true
						}
					}
				}
				if !found {
					if len(game.Rounds) > game.currentRound-1 {
						game.currentRound++
						game.currentStep = ChooseQuestion

						newDuration = 30 * time.Second
					} else {
						game.currentStep = Final
						newDuration = 5 * time.Minute

						var winnerID int
						var maxScore int
						for _, player := range game.players {
							if player.score > maxScore {
								maxScore = player.score
								winnerID = game.playersQueueIDByToken[player.client.token]
							}
						}

						game.broadcastServerEvent(AnswerDeclinedServer, nil, time.Now().In(time.UTC).Add(newDuration).Unix())
						game.broadcastServerEvent(FinalServer, FinalServerEvent{WinnerID: winnerID}, 0)
					}
				} else {
					game.currentStep = ChooseQuestion
					newDuration = 10 * time.Second
				}

				curQuest := game.Rounds[game.currentRound-1].Themes[game.currentTheme-1].Quests[game.currentQuestion-1]
				game.players[game.hub.clients[event.Token]].score -= curQuest.Price

				if len(game.players) > game.currentPlayerID {
					game.currentPlayerID++
				} else {
					game.currentPlayerID = 1
				}

				scoreChanged := ScoreChangedServerEvent{
					QueueID: game.currentPlayerID,
					Score:   game.players[game.hub.clients[event.Token]].score,
				}

				game.broadcastServerEvent(ScoreChangedServer, scoreChanged, time.Now().In(time.UTC).Add(newDuration).Unix())
			}

			if newDuration != 0 {
				ticker.Stop()
				ticker = time.NewTicker(newDuration)
			}
		case <-ticker.C:
			var newDuration time.Duration

			switch game.currentStep {
			case WaitingStart:
				singleton.DegTemporaryPack(game.UID)
				if !singleton.IsExistemporaryPack(game.UID) {
					err := os.Remove(game.configuration.PackTemporary.Path + "/" + string(game.UID[:]))
					if err != nil {
						// todo: logging
					}
				}

				game.hub.close <- struct{}{}

				break
			case Grettings:
				if len(game.Rounds) > game.currentRound {
					game.currentStep = ReadingRound
					game.currentRound++

					newDuration = 4 * time.Second
				} else {
					game.currentStep = Final

					newDuration = 5 * time.Minute

					game.broadcastServerEvent(FinalServer, FinalServerEvent{WinnerID: 1}, time.Now().In(time.UTC).Add(newDuration).Unix())
				}

				round := game.Rounds[game.currentRound-1]

				readingRound := ReadingRoundServerEvent{
					Name: round.Name,
				}

				game.broadcastServerEvent(ReadingRoundServer, readingRound, time.Now().In(time.UTC).Add(newDuration).Unix())
			case ReadingRound:
				game.currentStep = ReadingThemes

				round := game.Rounds[game.currentRound-1]

				newDuration = time.Duration(len(round.Themes)) * 3 * time.Second

				themeNames := make([]string, 0, len(round.Themes))
				for _, theme := range round.Themes {
					themeNames = append(themeNames, theme.Name)
				}

				readingThemes := ReadingThemesServerEvent{
					ThemeNames: themeNames,
				}

				game.broadcastServerEvent(ReadingThemesServer, readingThemes, time.Now().In(time.UTC).Add(newDuration).Unix())
			case ReadingThemes:
				game.currentStep = ChooseQuestion

				newDuration = 30 * time.Second

				round := game.Rounds[game.currentRound-1]

				wall := WallServerEvent{
					Themes: round.Themes,
				}

				game.broadcastServerEvent(WallServer, wall, time.Now().In(time.UTC).Add(newDuration).Unix())
			case ChooseQuestion:
				game.currentStep = Getting

				round := game.Rounds[game.currentRound-1]
				var themeID int
				var quest *Question
				for _, theme := range round.Themes {
					for _, question := range theme.Quests {
						if question.Price >= 0 {
							themeID = theme.Id
							quest = question
						}
					}
				}

				game.currentQuestion = quest.Id
				game.currentTheme = themeID

				getQuest := GetQuestServerEvent{
					QueueID: game.currentPlayerID,
				}

				newDuration = 10 * time.Second

				game.broadcastServerEvent(GetQuestServer, getQuest, time.Now().In(time.UTC).Add(newDuration).Unix())

				// todo: send correct answer to leader
			case Getting:
				var found bool
				for _, theme := range game.Rounds[game.currentRound-1].Themes {
					for _, question := range theme.Quests {
						if question.Price >= 0 && question.Id != game.currentQuestion {
							found = true
						}
					}
				}
				if !found {
					if len(game.Rounds) > game.currentRound-1 {
						game.currentRound++
						game.currentStep = ChooseQuestion

						newDuration = 30 * time.Second
					} else {
						game.currentStep = Final
						newDuration = 5 * time.Minute

						var winnerID int
						var maxScore int
						for _, player := range game.players {
							if player.score > maxScore {
								maxScore = player.score
								winnerID = game.playersQueueIDByToken[player.client.token]
							}
						}

						game.broadcastServerEvent(FinalServer, FinalServerEvent{WinnerID: winnerID}, time.Now().In(time.UTC).Add(newDuration).Unix())
					}
				} else {
					game.currentStep = ChooseQuestion
					newDuration = 10 * time.Second
				}

				currentQuest := game.Rounds[game.currentRound-1].Themes[game.currentTheme-1].Quests[game.currentQuestion-1]

				currentQuest.Price = -1

				wall := WallServerEvent{
					Themes: game.Rounds[game.currentRound-1].Themes,
				}

				game.broadcastServerEvent(WallServer, wall, time.Now().In(time.UTC).Add(newDuration).Unix())
			case Answering:
				var found bool
				for _, theme := range game.Rounds[game.currentRound-1].Themes {
					for _, question := range theme.Quests {
						if question.Price >= 0 && question.Id != game.currentQuestion {
							found = true
						}
					}
				}
				if !found {
					if len(game.Rounds) > game.currentRound-1 {
						game.currentRound++
						game.currentStep = ChooseQuestion

						newDuration = 10 * time.Second
					} else {
						game.currentStep = Final
						newDuration = 5 * time.Minute

						var winnerID int
						var maxScore int
						for _, player := range game.players {
							if player.score > maxScore {
								maxScore = player.score
								winnerID = game.playersQueueIDByToken[player.client.token]
							}
						}

						game.broadcastServerEvent(FinalServer, FinalServerEvent{WinnerID: winnerID}, time.Now().In(time.UTC).Add(newDuration).Unix())
					}
				} else {
					game.currentStep = ChooseQuestion
					newDuration = 10 * time.Second
				}

				curQuest := game.Rounds[game.currentRound-1].Themes[game.currentTheme-1].Quests[game.currentQuestion-1]
				game.players[game.hub.clients[game.playersTokenByQueueID[game.currentPlayerID]]].score -= curQuest.Price

				if len(game.players) > game.currentPlayerID {
					game.currentPlayerID++
				} else {
					game.currentPlayerID = 1
				}

				scoreChanged := ScoreChangedServerEvent{
					QueueID: game.currentPlayerID,
					Score:   game.players[game.hub.clients[game.playersTokenByQueueID[game.currentPlayerID]]].score,
				}

				game.broadcastServerEvent(ScoreChangedServer, scoreChanged, time.Now().In(time.UTC).Add(newDuration).Unix())
			case Final:
				singleton.DegTemporaryPack(game.UID)
				if !singleton.IsExistemporaryPack(game.UID) {
					err := os.Remove(game.configuration.PackTemporary.Path + "/" + string(game.UID[:]))
					if err != nil {
						// todo: logging
					}
				}
				game.hub.close <- struct{}{}

				break
			}

			if newDuration != 0 {
				ticker.Stop()
				ticker = time.NewTicker(newDuration)
			}
		}
	}
}

func (game *Game) broadcastServerEvent(eventType ServerEventType, event interface{}, exp int64) error {
	serverEvent := ServerEvent{
		Type: eventType,
		Exp:  exp,
		Data: event,
	}

	msg, err := json.Marshal(&serverEvent)
	if err != nil {
		return err
	}

	game.hub.broadcast <- msg

	return nil
}
