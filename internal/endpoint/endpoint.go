package endpoint

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/jmoiron/sqlx"
	"io"
	"io/ioutil"
	"mygame/config"
	"mygame/internal/models"
	"mygame/internal/repository"
	"mygame/internal/singleton"
	"mygame/tools/helpers"
	"mygame/tools/jwt"
	"net/http"
	"os"
	"time"
)

const (
	MB = 1 << 20

	MaxPackSize = MB * 150

	SiGame = "si_game_pack"
	MyGame = "my_game_pack"

	SiGameArchivesPath = "/siq_archives"

	ToArchiveType = ".zip"
)

type Endpoint struct {
	repository    *repository.Repository
	configuration *config.Config
}

func NewEndpoint(db *sqlx.DB, config *config.Config) *Endpoint {
	return &Endpoint{
		repository:    repository.NewRepository(db),
		configuration: config,
	}
}

func (e *Endpoint) InitRoutes() {
	http.HandleFunc("/auth/credentials", e.authCredentials)
	http.HandleFunc("/auth/access", e.authAccessToken)
	http.HandleFunc("/auth/guest", e.authGuest)
	http.HandleFunc("/get/login", e.getLoginFromAccessToken)
	http.HandleFunc("/register", e.createUser)
	http.HandleFunc("/hub", e.serveWs)
	http.HandleFunc("/pack/upload", e.saveSiGamePack)

}

func (e *Endpoint) saveSiGamePack(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseWriter(http.StatusMethodNotAllowed, map[string]interface{}{
			"error": "method not allowed",
		}, w)

		return
	}

	multipartFile, fileHeader, err := r.FormFile(SiGame)
	if err != nil {
		responseWriterError(err, w, http.StatusInternalServerError)

		return
	}

	_, err = jwt.ParseJWT([]byte(e.configuration.JWT.SecretKey), r.Header.Get("Authorization"))
	if err != nil {
		responseWriterError(err, w, http.StatusBadRequest)

		return
	}

	if fileHeader.Size > MaxPackSize {
		responseWriterError(errors.New("максимальный размер игры 150 MB"), w, http.StatusBadRequest)

		return
	}

	buf := bytes.NewBuffer(nil)
	if _, err := io.Copy(buf, multipartFile); err != nil {
		return
	}

	hash := sha256.Sum256(buf.Bytes())

	ok := singleton.IsExistPack(hash)
	if !ok {
		singleton.AddPack(hash, fileHeader.Filename)

		file, err := os.Create(e.configuration.Pack.Path + SiGameArchivesPath + "/" + fileHeader.Filename + ToArchiveType)
		if err != nil {
			responseWriterError(err, w, http.StatusInternalServerError)

			return
		}

		io.Copy(file, buf)
	}

	responseWriter(http.StatusOK, map[string]interface{}{}, w)

	return
}

func (e *Endpoint) authCredentials(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseWriter(http.StatusMethodNotAllowed, map[string]interface{}{
			"error": "method not allowed",
		}, w)

		return
	}

	var credentials *models.Credentials

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		responseWriterError(err, w, http.StatusInternalServerError)

		return
	}

	err = json.Unmarshal(body, &credentials)
	if err != nil {
		responseWriterError(err, w, http.StatusBadRequest)

		return
	}

	err = credentials.Validate()
	if err != nil {
		responseWriterError(err, w, http.StatusBadRequest)

		return
	}

	if !e.repository.UserRepository.IsExistByLogin(r.Context(), credentials.Login) {
		responseWriterError(err, w, http.StatusUnauthorized)

		return
	}

	hashPassword, err := helpers.NewMD5Hash(credentials.Password)
	if err != nil {
		responseWriterError(err, w, http.StatusInternalServerError)

		return
	}

	credentials.Password = hashPassword

	id, err := e.repository.UserRepository.GetUserByCredentials(r.Context(), credentials)
	if err != nil {
		responseWriterError(err, w, http.StatusUnauthorized)

		return
	}

	token, err := jwt.GenerateTokens(r.Context(), id, credentials.Login, e.configuration.JWT.SecretKey, e.configuration.JWT.ExpirationTime)
	if err != nil {
		responseWriterError(err, w, http.StatusInternalServerError)

		return
	}

	responseWriter(http.StatusOK, map[string]interface{}{
		"access_token": token,
	}, w)

	return
}

func (e *Endpoint) authAccessToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseWriter(http.StatusMethodNotAllowed, map[string]interface{}{
			"error": "method not allowed",
		}, w)

		return
	}

	type request struct {
		AccessToken string `json:"access_token"`
	}

	var req *request

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		responseWriterError(err, w, http.StatusInternalServerError)

		return
	}

	err = json.Unmarshal(body, &req)
	if err != nil {
		responseWriterError(err, w, http.StatusBadRequest)

		return
	}

	token, err := jwt.ParseJWT([]byte(e.configuration.JWT.SecretKey), req.AccessToken)
	if err != nil {
		responseWriterError(err, w, http.StatusBadRequest)

		return
	}

	if token.ExpiresAt < time.Now().Unix() {
		responseWriterError(errors.New("token has expired"), w, http.StatusUnauthorized)

		return
	}

	responseWriter(http.StatusOK, map[string]interface{}{}, w)

	return
}

func (e *Endpoint) authGuest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseWriter(http.StatusMethodNotAllowed, map[string]interface{}{
			"error": "method not allowed",
		}, w)

		return
	}

	type request struct {
		Login string `json:"login"`
	}

	var req *request

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		responseWriterError(err, w, http.StatusInternalServerError)

		return
	}

	err = json.Unmarshal(body, &req)
	if err != nil {
		responseWriterError(err, w, http.StatusBadRequest)

		return
	}

	token, err := jwt.GenerateTokens(r.Context(), 0, req.Login, e.configuration.JWT.SecretKey, e.configuration.JWT.ExpirationTime)
	if err != nil {
		responseWriterError(err, w, http.StatusInternalServerError)

		return
	}

	responseWriter(http.StatusOK, map[string]interface{}{
		"access_token": token,
	}, w)

	return
}

func (e *Endpoint) getLoginFromAccessToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseWriter(http.StatusMethodNotAllowed, map[string]interface{}{
			"error": "method not allowed",
		}, w)

		return
	}

	type request struct {
		AccessToken string `json:"access_token"`
	}

	var req *request

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		responseWriterError(err, w, http.StatusInternalServerError)

		return
	}

	err = json.Unmarshal(body, &req)
	if err != nil {
		responseWriterError(err, w, http.StatusBadRequest)

		return
	}

	token, err := jwt.ParseJWT([]byte(e.configuration.JWT.SecretKey), req.AccessToken)
	if err != nil {
		responseWriterError(err, w, http.StatusBadRequest)

		return
	}

	if token.ExpiresAt < time.Now().Unix() {
		responseWriterError(errors.New("token has expired"), w, http.StatusUnauthorized)

		return
	}

	responseWriter(http.StatusOK, map[string]interface{}{
		"login": token.Login,
	}, w)

	return
}

func (e *Endpoint) createUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseWriter(http.StatusMethodNotAllowed, map[string]interface{}{
			"error": "method not allowed",
		}, w)

		return
	}

	var user *models.User

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		responseWriterError(err, w, http.StatusInternalServerError)

		return
	}

	err = json.Unmarshal(body, &user)
	if err != nil {
		responseWriterError(err, w, http.StatusBadRequest)

		return
	}

	if e.repository.UserRepository.IsExistByLogin(r.Context(), user.Login) {
		responseWriterError(err, w, http.StatusBadRequest)

		return
	}

	hashPassword, err := helpers.NewMD5Hash(user.Password)
	if err != nil {
		responseWriterError(err, w, http.StatusInternalServerError)

		return
	}

	user.Password = hashPassword

	id, err := e.repository.UserRepository.CreateUser(r.Context(), user)
	if err != nil {
		responseWriterError(err, w, http.StatusInternalServerError)

		return
	}

	token, err := jwt.GenerateTokens(r.Context(), id, user.Login, e.configuration.JWT.SecretKey, e.configuration.JWT.ExpirationTime)
	if err != nil {
		responseWriterError(err, w, http.StatusInternalServerError)

		return
	}

	responseWriter(http.StatusOK, map[string]interface{}{
		"access_token": token,
	}, w)

	return
}
