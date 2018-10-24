package blockchain

import (
	"fmt"
	"github.com/hyperledger/fabric-sdk-go/pkg/client/channel"
	"github.com/hyperledger/fabric-sdk-go/pkg/client/event"
	mspclient "github.com/hyperledger/fabric-sdk-go/pkg/client/msp"
	"github.com/hyperledger/fabric-sdk-go/pkg/client/resmgmt"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/errors/retry"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/msp"
	"github.com/hyperledger/fabric-sdk-go/pkg/core/config"
	packager "github.com/hyperledger/fabric-sdk-go/pkg/fab/ccpackager/gopackager"
	"github.com/hyperledger/fabric-sdk-go/pkg/fabsdk"
	"github.com/hyperledger/fabric-sdk-go/third_party/github.com/hyperledger/fabric/common/cauthdsl"
	"github.com/pkg/errors"
)

// FabricSetup implementation
type FabricSetup struct {
	initialized   bool
	ConfigFile    string
	Org           Org
	ChannelConfig ChannelConfig
	ChainCode     ChainCode
	Util          Util
}

// Org 组织信息
type Org struct {
	ID        string
	Admin     string
	Name      string
	User      string
	OrderID string
}

// ChannelConfig 通道配置信息
type ChannelConfig struct {
	ID       string
	FilePath string
}

// ChainCode 链码信息
type ChainCode struct {
	ID      string
	Version string
	GoPath  string
	SrcPath string
}

// Util 工具
type Util struct {
	client *channel.Client
	admin  *resmgmt.Client
	sdk    *fabsdk.FabricSDK
	event  *event.Client
}

// Initialize reads the configuration file and sets up the client, chain and event hub
func (setup *FabricSetup) Initialize() error {

	// Add parameters for the initialization
	if setup.initialized {
		return errors.New("sdk already initialized")
	}

	// Initialize the SDK with the configuration file
	sdk, err := fabsdk.New(config.FromFile(setup.ConfigFile))
	if err != nil {
		return errors.WithMessage(err, "failed to create SDK")
	}
	setup.Util.sdk = sdk
	fmt.Println("SDK created")

	// The resource management client is responsible for managing channels (create/update channel)
	resourceManagerClientContext := setup.Util.sdk.Context(fabsdk.WithUser(setup.Org.Admin), fabsdk.WithOrg(setup.Org.Name))
	if err != nil {
		return errors.WithMessage(err, "failed to load Admin identity")
	}
	resMgmtClient, err := resmgmt.New(resourceManagerClientContext)
	if err != nil {
		return errors.WithMessage(err, "failed to create channel management client from Admin identity")
	}
	setup.Util.admin = resMgmtClient
	fmt.Println("Resource management client created")

	// The MSP client allow us to retrieve user information from their identity, like its signing identity which we will need to save the channel
	mspClient, err := mspclient.New(sdk.Context(), mspclient.WithOrg(setup.Org.Name))
	if err != nil {
		return errors.WithMessage(err, "failed to create MSP client")
	}
	adminIdentity, err := mspClient.GetSigningIdentity(setup.Org.Admin)
	if err != nil {
		return errors.WithMessage(err, "failed to get admin signing identity")
	}
	req := resmgmt.SaveChannelRequest{ChannelID: setup.ChannelConfig.ID, ChannelConfigPath: setup.ChannelConfig.FilePath, SigningIdentities: []msp.SigningIdentity{adminIdentity}}
	txID, err := setup.Util.admin.SaveChannel(req, resmgmt.WithOrdererEndpoint(setup.Org.OrderID))
	if err != nil || txID.TransactionID == "" {
		return errors.WithMessage(err, "failed to save channel")
	}
	fmt.Println("Channel created")

	// Make admin user join the previously created channel
	if err = setup.Util.admin.JoinChannel(setup.ChannelConfig.ID, resmgmt.WithRetry(retry.DefaultResMgmtOpts), resmgmt.WithOrdererEndpoint(setup.Org.OrderID)); err != nil {
		return errors.WithMessage(err, "failed to make admin join channel")
	}
	fmt.Println("Channel joined")

	fmt.Println("Initialization Successful")
	setup.initialized = true
	return nil
}

// InstallAndInstantiateCC 安装与初始化ChainCode
func (setup *FabricSetup) InstallAndInstantiateCC() error {
	chainCode := setup.ChainCode

	// Create the ChainCode package that will be sent to the peers
	ccPkg, err := packager.NewCCPackage(chainCode.SrcPath, chainCode.GoPath)
	if err != nil {
		return errors.WithMessage(err, "failed to create ChainCode package")
	}
	fmt.Println("ccPkg created")

	// Install example cc to org peers
	installCCReq := resmgmt.InstallCCRequest{Name: chainCode.ID, Path: chainCode.SrcPath, Version: chainCode.Version, Package: ccPkg}
	_, err = setup.Util.admin.InstallCC(installCCReq, resmgmt.WithRetry(retry.DefaultResMgmtOpts))
	if err != nil {
		return errors.WithMessage(err, "failed to install chaincode")
	}
	fmt.Println("ChainCode installed")

	// Set up ChainCode policy
	ccPolicy := cauthdsl.SignedByAnyMember([]string{setup.Org.ID})

	resp, err := setup.Util.admin.InstantiateCC(setup.ChannelConfig.ID, resmgmt.InstantiateCCRequest{Name: chainCode.ID, Path: chainCode.SrcPath, Version: chainCode.Version, Args: [][]byte{[]byte("init")}, Policy: ccPolicy})
	if err != nil || resp.TransactionID == "" {
		return errors.WithMessage(err, "failed to instantiate the chaincode")
	}
	fmt.Println("ChainCode instantiated")

	// Channel client is used to query and execute transactions
	clientContext := setup.Util.sdk.ChannelContext(setup.ChannelConfig.ID, fabsdk.WithUser(setup.Org.User))
	setup.Util.client, err = channel.New(clientContext)
	if err != nil {
		return errors.WithMessage(err, "failed to create new channel client")
	}
	fmt.Println("Channel client created")

	// Creation of the client which will enables access to our channel events
	setup.Util.event, err = event.New(clientContext)
	if err != nil {
		return errors.WithMessage(err, "failed to create new event client")
	}
	fmt.Println("Event client created")

	fmt.Println("ChainCode Installation & Instantiation Successful")
	return nil
}
