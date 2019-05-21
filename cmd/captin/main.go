package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"

	core "github.com/shoplineapp/captin/core"
	models "github.com/shoplineapp/captin/internal/models"
)

func main() {
	fmt.Println("* Starting captin (Press ENTER to quit)")

	pwd, _ := os.Getwd()
	path := os.Args[1:][0]
	absPath := filepath.Join(pwd, path)

	mappedConfigs := models.NewConfigurationMapperFromPath(absPath)

	captin := core.NewCaptin(*configMapper)
	captin.Execute(models.IncomingEvent{
		Key:        "product.update",
		Source:     "core",
		Payload:    map[string]interface{}{"field1": 1},
		TargetType: "Product",
		TargetId:   "product_id",
	})

	bufio.NewReader(os.Stdin).ReadBytes('\n')
}
