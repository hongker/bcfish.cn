package main

import (
	"bcfish.cn/demo/web/blockchain"
	"fmt"
	"github.com/gin-gonic/gin"
	"os"
)

func main() {
	goPath := os.Getenv("GOPATH")
	fabricSetup := blockchain.FabricSetup{
		ConfigFile: "config.yaml",

		Org: blockchain.Org{
			ID: "Org1MSP",
			Name: "org1",
			Admin: "Admin",
			User: "User1",
			OrdererID: "orderer.example.com",
		},
		ChannelConfig:blockchain.ChannelConfig{
			ID: "mychannel",
			FilePath: goPath + "/src/bcfish.cn/demo/artifacts/channel/mychannel.tx",
		},
		ChainCode:blockchain.ChainCode{
			ID: "example_cc",
			Version: "0.1",
			GoPath: goPath,
			SrcPath: "bcfish.cn/demo/artifacts/src/go/",
		},

	}

	if err := fabricSetup.Initialize();err != nil {
		fmt.Printf("Unable to initialize the Fabric SDK: %v\n", err)
		return
	}


	if err := fabricSetup.InstallAndInstantiateCC();err != nil {
		fmt.Printf("Unable to install and instantiate the chaincode: %v\n", err)
		return
	}

	r := gin.Default()
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
		})
	})
	r.Run() // listen and serve on 0.0.0.0:8080

}

