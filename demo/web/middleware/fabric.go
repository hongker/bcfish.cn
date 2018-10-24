package middleware

import (
	"bcfish.cn/demo/web/blockchain"
	"fmt"
	"os"
)
var (
	goPath = os.Getenv("GOPATH") // golang路径
)

// GetFabricSetupInstance 获取fabric初始化的实例
func GetFabricSetupInstance() *blockchain.FabricSetup {
	fabricSetup := blockchain.FabricSetup {
		ConfigFile: "config.yaml",

		Org: blockchain.Org{
			ID: "org1.example.com",
			Name: "org1",
			Admin: "Admin",
			User: "User1",
			OrderID: "orderer.example.com",
		},
		ChannelConfig:blockchain.ChannelConfig{
			ID: "mychannel",
			FilePath: goPath + "/src/bcfish.cn/demo/artifacts/channel/mychannel.tx",
		},

	}

	return &fabricSetup
}

// InitExampleCC 初始化 example链码
func InitExampleCC(fabricSetup *blockchain.FabricSetup) (*blockchain.FabricSetup, error) {
	temp := &fabricSetup

	setup := *temp
	setup.ChainCode = blockchain.ChainCode{
		ID: "example_cc",
		Version: "0.1",
		GoPath: goPath,
		SrcPath: "bcfish.cn/demo/artifacts/src/go/",
	}

	if err := setup.InstallAndInstantiateCC();err != nil {
		return nil, fmt.Errorf("Unable to install and instantiate the chaincode: %v\n", err)
	}

	return setup, nil

}