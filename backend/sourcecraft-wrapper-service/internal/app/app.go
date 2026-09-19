package app

import (
	"code_health/internal/config"
	"code_health/internal/service"
	"log"
	"net/http"
	"strconv"
	"time"
)

func StartApp() {
	cfg := config.LoadConfig()
	var client http.Client
	timeout, err := strconv.Atoi(cfg.ClientTimeout)
	if err != nil {
		log.Println("Invalid client timeout: ", err)
		return
	}
	client = http.Client{
		Timeout: time.Duration(timeout),
	}
	service := service.NewSCWrapperService(client, cfg.SourceCraftURL)
}
