package blockchain

import (
	"github.com/hyperledger/fabric-sdk-go/pkg/client/channel"
	"github.com/hyperledger/fabric-sdk-go/pkg/client/event"
	mspclient "github.com/hyperledger/fabric-sdk-go/pkg/client/msp"
	"github.com/hyperledger/fabric-sdk-go/pkg/client/resmgmt"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/errors/retry"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/msp"
	"github.com/hyperledger/fabric-sdk-go/pkg/core/config"
	"github.com/hyperledger/fabric-sdk-go/pkg/fabsdk"
	"github.com/pkg/errors"
	packager "github.com/hyperledger/fabric-sdk-go/pkg/fab/ccpackager/gopackager"
	"github.com/hyperledger/fabric-sdk-go/third_party/github.com/hyperledger/fabric/common/cauthdsl"
	"fmt"
	"github.com/hyperledger/fabric-sdk-go/pkg/client/ledger"
	"strings"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/core"
	"github.com/hyperledger/fabric-sdk-go/pkg/core/config/lookup"

	fabApi "github.com/hyperledger/fabric-sdk-go/pkg/common/providers/fab"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/errors/status"
	contextImpl "github.com/hyperledger/fabric-sdk-go/pkg/context"
	contextAPI "github.com/hyperledger/fabric-sdk-go/pkg/common/providers/context"
)

// FabricSetup implementation
type FabricSetup struct {
	ConfigFile      string
	OrgID           string
	OrgAdmin        string
	OrgName         string
	UserName        string
	OrdererID       string
	ChannelConfig
	ChainCode
	client          *channel.Client
	admin           *resmgmt.Client
	sdk             *fabsdk.FabricSDK
	event           *event.Client
	ledger			*ledger.Client
	initialized     bool
}

type ChainCode struct {
	ID string
	GoPath string
	SrcPath string
	Version string
}

type ChannelConfig struct {
	ID string
	FilePath string
}

type Msg struct{
	StatusCode 	int	`json:"status_code"`
	Message string	`json:"message"`
	Data interface{} `json:"data"`
}

func GetParams(args []string) [][]byte {
	var res [][]byte

	for _, item := range args {
		res = append(res, []byte(item))
	}

	return res
}


// Initialize reads the configuration file and sets up the client, chain and event hub
func (setup *FabricSetup) Initialize() error {
	fmt.Println("currentOrg:", setup.OrgName)
	// Add parameters for the initialization
	if setup.initialized {
		return errors.New("sdk already initialized")
	}

	// Initialize the SDK with the configuration file
	sdk, err := fabsdk.New(config.FromFile(setup.ConfigFile))
	if err != nil {
		return errors.WithMessage(err, "failed to create SDK")
	}
	setup.sdk = sdk
	fmt.Println("SDK created")

	// The resource management client is responsible for managing channels (create/update channel)
	resourceManagerClientContext := setup.sdk.Context(fabsdk.WithUser(setup.OrgAdmin), fabsdk.WithOrg(setup.OrgName))
	if err != nil {
		return errors.WithMessage(err, "failed to load Admin identity")
	}
	resMgmtClient, err := resmgmt.New(resourceManagerClientContext)
	if err != nil {
		return errors.WithMessage(err, "failed to create channel management client from Admin identity")
	}
	setup.admin = resMgmtClient
	fmt.Println("Resource management client created")

	// The MSP client allow us to retrieve user information from their identity, like its signing identity which we will need to save the channel
	mspClient, err := mspclient.New(sdk.Context(), mspclient.WithOrg(setup.OrgName))
	if err != nil {
		return errors.WithMessage(err, "failed to create MSP client")
	}
	adminIdentity, err := mspClient.GetSigningIdentity(setup.OrgAdmin)
	if err != nil {
		return errors.WithMessage(err, "failed to get admin signing identity")
	}

	if setup.checkIsJoinedChannel() {
		fmt.Println("Channel has joined")
	}else {
		req := resmgmt.SaveChannelRequest{ChannelID: setup.ChannelConfig.ID, ChannelConfigPath: setup.ChannelConfig.FilePath, SigningIdentities: []msp.SigningIdentity{adminIdentity}}
		txID, err := setup.admin.SaveChannel(req, resmgmt.WithOrdererEndpoint(setup.OrdererID))
		if err != nil || txID.TransactionID == "" {
			return errors.WithMessage(err, "failed to save channel")
		}
		fmt.Println("Channel created")

		// Make admin user join the previously created channel
		if err = setup.admin.JoinChannel(setup.ChannelConfig.ID, resmgmt.WithRetry(retry.DefaultResMgmtOpts), resmgmt.WithOrdererEndpoint(setup.OrdererID)); err != nil {
			return errors.WithMessage(err, "failed to make admin join channel")
		}
		fmt.Println("Channel joined")
	}


	// Channel client is used to query and execute transactions
	clientContext := setup.sdk.ChannelContext(setup.ChannelConfig.ID, fabsdk.WithUser(setup.UserName))
	setup.client, err = channel.New(clientContext)
	if err != nil {
		return errors.WithMessage(err, "failed to create new channel client")
	}
	fmt.Println("Channel client created")

	// Creation of the client which will enables access to our channel events
	setup.event, err = event.New(clientContext)
	if err != nil {
		return errors.WithMessage(err, "failed to create new event client")
	}
	fmt.Println("Event client created")

	setup.ledger, err = ledger.New(clientContext)
	if err != nil {
		return errors.WithMessage(err, "failed to create new ledger client")
	}
	fmt.Println("Ledger client created")

	fmt.Println("Initialization Successful")
	setup.initialized = true
	return nil
}

func (setup *FabricSetup) checkIsJoinedChannel() bool {
	var provider contextAPI.ClientProvider

	provider = setup.sdk.Context(fabsdk.WithUser(setup.OrgAdmin), fabsdk.WithOrg(setup.OrgName))
	orgPeers, err := DiscoverLocalPeers(provider, 2)
	if err != nil {
		fmt.Println(err.Error())
		return false
	}

	joined, err := IsJoinedChannel(setup.ChannelConfig.ID, setup.admin, orgPeers[0])
	if err != nil {
		fmt.Println(err.Error())
		return false
	}

	return joined
}

func (setup *FabricSetup) InstallAndInstantiateCC() error {

	// Create the chaincode package that will be sent to the peers
	ccPkg, err := packager.NewCCPackage(setup.ChainCode.SrcPath, setup.ChainCode.GoPath)
	if err != nil {
		return errors.WithMessage(err, "failed to create chaincode package")
	}
	fmt.Println("ccPkg created")

	if setup.checkCCInstalled() {
		fmt.Println("Chaincode has installed")
		ccPolicy := cauthdsl.SignedByMspMember(setup.OrgID)
		updateCCReq := resmgmt.UpgradeCCRequest{Name: setup.ChainCode.ID, Path: setup.ChainCode.SrcPath, Version: setup.ChainCode.Version, Policy: ccPolicy}
		_, err = setup.admin.UpgradeCC(setup.ChannelConfig.ID, updateCCReq, resmgmt.WithRetry(retry.DefaultResMgmtOpts))
		if err != nil {
			return errors.WithMessage(err, "failed to upgrade chaincode")
		}
		fmt.Println("Chaincode upgrade")
	}else {


		// Install example cc to org peers
		installCCReq := resmgmt.InstallCCRequest{Name: setup.ChainCode.ID, Path: setup.ChainCode.SrcPath, Version: setup.ChainCode.Version, Package: ccPkg}
		_, err = setup.admin.InstallCC(installCCReq, resmgmt.WithRetry(retry.DefaultResMgmtOpts))
		if err != nil {
			return errors.WithMessage(err, "failed to install chaincode")
		}
		fmt.Println("Chaincode installed")
	}


	if setup.checkCCInstantiated() {
		fmt.Println("Chaincode has Instantiated")
	}else {
		// Set up chaincode policy
		ccPolicy := cauthdsl.SignedByAnyMember([]string{setup.OrgID})

		resp, err := setup.admin.InstantiateCC(setup.ChannelConfig.ID, resmgmt.InstantiateCCRequest{Name: setup.ChainCode.ID, Path: setup.ChainCode.GoPath, Version: setup.ChainCode.Version, Args: [][]byte{[]byte("init")}, Policy: ccPolicy})
		if err != nil || resp.TransactionID == "" {
			return errors.WithMessage(err, "failed to instantiate the chaincode")
		}
		fmt.Println("Chaincode instantiated")
	}


	fmt.Println("Chaincode Installation & Instantiation Successful")
	return nil
}

func OrgTargetPeers(orgs []string, configBackend ...core.ConfigBackend) ([]string, error) {
	networkConfig := fabApi.NetworkConfig{}
	err := lookup.New(configBackend...).UnmarshalKey("organizations", &networkConfig.Organizations)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to get organizations from config ")
	}

	var peers []string
	for _, org := range orgs {
		orgConfig, ok := networkConfig.Organizations[strings.ToLower(org)]
		if !ok {
			continue
		}
		peers = append(peers, orgConfig.Peers...)
	}
	return peers, nil
}

func (setup *FabricSetup) RegisterAdmin(accountID, password string){

}

func (setup *FabricSetup) checkCCInstalled() bool {
	var provider contextAPI.ClientProvider

	provider = setup.sdk.Context(fabsdk.WithUser(setup.OrgAdmin), fabsdk.WithOrg(setup.OrgName))
	orgPeers, err := DiscoverLocalPeers(provider, 2)
	if err != nil {
		fmt.Println(err.Error())
		return false
	}

	installed := isCCInstalled(setup.OrgName, setup.admin, setup.ChainCode.ID, setup.ChainCode.Version, orgPeers)

	return installed
}

func (setup *FabricSetup) checkCCInstantiated() bool {
	res, err := isCCInstantiated(setup.admin, setup.ChannelConfig.ID,  setup.ChainCode.ID, setup.ChainCode.Version)
	if err != nil {
		fmt.Println(err.Error())
		return false
	}

	return res
}

// DiscoverLocalPeers queries the local peers for the given MSP context and returns all of the peers. If
// the number of peers does not match the expected number then an error is returned.
func DiscoverLocalPeers(ctxProvider contextAPI.ClientProvider, expectedPeers int) ([]fabApi.Peer, error) {
	ctx, err := contextImpl.NewLocal(ctxProvider)
	if err != nil {
		return nil, errors.Wrap(err, "error creating local context")
	}

	discoveredPeers, err := retry.NewInvoker(retry.New(retry.TestRetryOpts)).Invoke(
		func() (interface{}, error) {
			peers, err := ctx.LocalDiscoveryService().GetPeers()
			if err != nil {
				return nil, errors.Wrapf(err, "error getting peers for MSP [%s]", ctx.Identifier().MSPID)
			}
			if len(peers) < expectedPeers {
				return nil, status.New(status.TestStatus, status.GenericTransient.ToInt32(), fmt.Sprintf("Expecting %d peers but got %d", expectedPeers, len(peers)), nil)
			}
			return peers, nil
		},
	)

	if err != nil {
		return nil, err
	}

	return discoveredPeers.([]fabApi.Peer), nil
}


func isCCInstalled(orgID string, resMgmt *resmgmt.Client, ccName, ccVersion string, peers []fabApi.Peer) bool {
	fmt.Println("OrgID:", orgID)
	installedOnAllPeers := true
	for _, peer := range peers {
		fmt.Printf("Querying [%s] ...\n", peer.URL())
		resp, err := resMgmt.QueryInstalledChaincodes(resmgmt.WithTargets(peer))
		fmt.Println(err)

		found := false
		for _, ccInfo := range resp.Chaincodes {
			fmt.Printf("... found chaincode [%s:%s]", ccInfo.Name, ccInfo.Version)
			if ccInfo.Name == ccName && ccInfo.Version == ccVersion {
				found = true
				break
			}
		}
		if !found {
			fmt.Printf("... chaincode [%s:%s] is not installed on peer [%s]\n", ccName, ccVersion, peer.URL())
			installedOnAllPeers = false
		}
	}
	return installedOnAllPeers
}

func isCCInstantiated(resMgmt *resmgmt.Client, channelID, ccName, ccVersion string) (bool, error) {
	chaincodeQueryResponse, err := resMgmt.QueryInstantiatedChaincodes(channelID, resmgmt.WithRetry(retry.DefaultResMgmtOpts))
	if err != nil {
		return false, errors.WithMessage(err, "Query for instantiated chaincodes failed")
	}

	for _, chaincode := range chaincodeQueryResponse.Chaincodes {
		if chaincode.Name == ccName && chaincode.Version == ccVersion {
			return true, nil
		}
	}
	return false, nil
}

func IsJoinedChannel(channelID string, resMgmtClient *resmgmt.Client, peer fabApi.Peer) (bool, error) {
	resp, err := resMgmtClient.QueryChannels(resmgmt.WithTargets(peer))
	if err != nil {
		return false, err
	}
	for _, chInfo := range resp.Channels {
		if chInfo.ChannelId == channelID {
			return true, nil
		}
	}
	return false, nil
}
