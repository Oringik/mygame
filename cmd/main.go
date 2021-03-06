package main

import (
	"flag"
	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"log"
	"mygame/config"
	"mygame/dependers/database"
	"mygame/dependers/logger"
	"mygame/dependers/monitoring"
	"mygame/internal/endpoint"
	"mygame/internal/singleton"
	"mygame/tools/helpers"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

const defaultPacksPath = "./packs"
const defaultPacksTemporaryPath = "./packs_temporary"

var packsPath string
var packsTemporaryPath string

func init() {
	flag.StringVar(&packsPath, "packs-path", defaultPacksPath, "packs path")
	flag.StringVar(&packsTemporaryPath, "packs-temp-path", defaultPacksTemporaryPath, "packs temporary path")
	flag.StringVar(&packsPath, "p", defaultPacksPath, "packs path")
}

func main() {
	secretKey := os.Getenv("SECRET_KEY")

	flag.Parse()

	config, err := parseCfg("./config/config.yaml")
	if err != nil {
		panic(err)
	}

	config.Pack.Path = packsPath
	config.PackTemporary.Path = packsTemporaryPath
	config.JWT.SecretKey = secretKey

	connectionAddr := &database.Connection{
		Host:     config.DB.Host,
		Port:     config.DB.Port,
		User:     config.DB.User,
		Password: config.DB.Password,
		DBName:   config.DB.DBName,
		SSLMode:  config.DB.SSLMode,
	}

	connectionAddrStr := database.GenerateAddr(connectionAddr)

	db, err := database.NewDB(connectionAddrStr)
	if err != nil {
		log.Fatal(err)
	}

	logger, err := logger.ConfigureLogger(config.App.LogLevel)
	if err != nil {
		log.Fatal(err)
	}

	logger = logger.With(zap.String("app_token", helpers.GenerateRandomString(16)))

	singleton.InitSingleton()

	singleton.InitPacks(defaultPacksPath + "/" + "siq_archives")

	log.Println(singleton.GetPacks())

	monitoring := monitoring.NewPrometheusMonitoring(config.Monitoring)

	endpoint := endpoint.NewEndpoint(db, config, logger, monitoring)
	endpoint.InitRoutes()

	logger.Info(
		"My game server started",
		zap.Int("port", config.App.Port),
		zap.String("log_level", config.App.LogLevel),
		zap.String("database_name", config.DB.DBName),
		zap.String("database_host", config.DB.Host+":"+config.DB.Port),
	)

	log.Fatal(http.ListenAndServe(":"+strconv.Itoa(config.App.Port), nil))
}

func parseCfg(path string) (*config.Config, error) {
	filename, _ := filepath.Abs(path)
	yamlFile, err := ioutil.ReadFile(filename)

	if err != nil {
		return nil, err
	}

	var config config.Config

	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}
