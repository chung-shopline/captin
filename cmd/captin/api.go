package main

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"os"
	"path/filepath"

	core "github.com/shoplineapp/captin/core"
	incoming "github.com/shoplineapp/captin/incoming"
	models "github.com/shoplineapp/captin/models"
)

func main() {
	port := ":9457"

	// Load webhooks configuration
	pwd, _ := os.Getwd()
	path := os.Args[1:][0]
	absPath := filepath.Join(pwd, path)
	configMapper := models.NewConfigurationMapperFromPath(absPath)

	captin := core.NewCaptin(*configMapper)

	// Set up api server
	router := gin.Default()
	handler := incoming.HttpEventHandler{}
	handler.Setup(*captin)
	handler.SetRoutes(router)

	fmt.Printf("* Binding captin on 0.0.0.0%s\n", port)
	router.Run(port)
}