package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"os"
	"time"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/consensys/gnark-crypto/ecc/stark-curve/fp"
	"github.com/spf13/cobra"
)

func CreateRootCommand() *cobra.Command {
	// rootCmd represents the base command when called without any subcommands
	rootCmd := &cobra.Command{
		Use:   "influence-eth",
		Short: "Influence.eth leaderboards by Moonstream",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
		},
	}

	completionCmd := CreateCompletionCommand(rootCmd)
	versionCmd := CreateVersionCommand()
	eventsCmd := CreateEventsCommand()
	findDeploymentBlockCmd := CreateFindDeploymentCmd()
	parseCmd := CreateParseCommand()
	leaderboardCmd := CreateLeaderboardCommand()
	leaderboardsCmd := CreateLeaderboardsCommand()
	rootCmd.AddCommand(completionCmd, versionCmd, eventsCmd, findDeploymentBlockCmd, parseCmd, leaderboardCmd, leaderboardsCmd)

	// By default, cobra Command objects write to stderr. We have to forcibly set them to output to
	// stdout.
	rootCmd.SetOut(os.Stdout)

	return rootCmd
}

func CreateCompletionCommand(rootCmd *cobra.Command) *cobra.Command {
	completionCmd := &cobra.Command{
		Use:   "completion",
		Short: "Generate shell completion scripts for influence-eth",
		Long: `Generate shell completion scripts for influence-eth.

The command for each shell will print a completion script to stdout. You can source this script to get
completions in your current shell session. You can add this script to the completion directory for your
shell to get completions for all future sessions.

For example, to activate bash completions in your current shell:
		$ . <(influence-eth completion bash)

To add influence-eth completions for all bash sessions:
		$ influence-eth completion bash > /etc/bash_completion.d/influence-eth_completions`,
	}

	bashCompletionCmd := &cobra.Command{
		Use:   "bash",
		Short: "bash completions for influence-eth",
		Run: func(cmd *cobra.Command, args []string) {
			rootCmd.GenBashCompletion(cmd.OutOrStdout())
		},
	}

	zshCompletionCmd := &cobra.Command{
		Use:   "zsh",
		Short: "zsh completions for influence-eth",
		Run: func(cmd *cobra.Command, args []string) {
			rootCmd.GenZshCompletion(cmd.OutOrStdout())
		},
	}

	fishCompletionCmd := &cobra.Command{
		Use:   "fish",
		Short: "fish completions for influence-eth",
		Run: func(cmd *cobra.Command, args []string) {
			rootCmd.GenFishCompletion(cmd.OutOrStdout(), true)
		},
	}

	powershellCompletionCmd := &cobra.Command{
		Use:   "powershell",
		Short: "powershell completions for influence-eth",
		Run: func(cmd *cobra.Command, args []string) {
			rootCmd.GenPowerShellCompletion(cmd.OutOrStdout())
		},
	}

	completionCmd.AddCommand(bashCompletionCmd, zshCompletionCmd, fishCompletionCmd, powershellCompletionCmd)

	return completionCmd
}

func CreateVersionCommand() *cobra.Command {
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version of influence-eth that you are currently using",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Println(Version)
		},
	}

	return versionCmd
}

func CreateEventsCommand() *cobra.Command {
	var providerURL, contractAddress string
	var timeout, fromBlock, toBlock uint64
	var batchSize, coldInterval, hotInterval, hotThreshold, confirmations int

	eventsCmd := &cobra.Command{
		Use:   "events",
		Short: "Crawl events from your Starknet RPC provider",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if providerURL == "" {
				providerURLFromEnv := os.Getenv("STARKNET_RPC_URL")
				if providerURLFromEnv == "" {
					return errors.New("you must provide a provider URL using -p/--provider or set the STARKNET_RPC_URL environment variable")
				}
				providerURL = providerURLFromEnv
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, clientErr := rpc.NewClient(providerURL)
			if clientErr != nil {
				return clientErr
			}

			provider := rpc.NewProvider(client)
			ctx := context.Background()

			eventsChan := make(chan RawEvent)

			// If "fromBlock" is not specified, find the block at which the contract was deployed and
			// use that instead.
			if fromBlock == 0 {
				addressFelt, parseAddressErr := FeltFromHexString(contractAddress)
				if parseAddressErr != nil {
					return parseAddressErr
				}
				deploymentBlock, fromBlockErr := DeploymentBlock(ctx, provider, addressFelt)
				if fromBlockErr != nil {
					return fromBlockErr
				}
				fromBlock = deploymentBlock
			}

			go ContractEvents(ctx, provider, contractAddress, eventsChan, hotThreshold, time.Duration(hotInterval)*time.Millisecond, time.Duration(coldInterval)*time.Millisecond, fromBlock, toBlock, confirmations, batchSize)

			for event := range eventsChan {
				unparsedEvent := ParsedEvent{Name: EVENT_UNKNOWN, Event: event}
				serializedEvent, marshalErr := json.Marshal(unparsedEvent)
				if marshalErr != nil {
					cmd.ErrOrStderr().Write([]byte(marshalErr.Error()))
				}
				cmd.Println(string(serializedEvent))
			}

			return nil
		},
	}

	eventsCmd.PersistentFlags().StringVarP(&providerURL, "provider", "p", "", "The URL of your Starknet RPC provider (defaults to value of STARKNET_RPC_URL environment variable)")
	eventsCmd.PersistentFlags().Uint64VarP(&timeout, "timeout", "t", 0, "The timeout for requests to your Starknet RPC provider")
	eventsCmd.Flags().StringVarP(&contractAddress, "contract", "c", "", "The address of the contract from which to crawl events (if not provided, no contract constraint will be specified)")
	eventsCmd.Flags().IntVarP(&batchSize, "batch-size", "N", 100, "The number of events to fetch per batch (defaults to 100)")
	eventsCmd.Flags().IntVar(&hotThreshold, "hot-threshold", 2, "Number of successive iterations which must return events before we consider the crawler hot")
	eventsCmd.Flags().IntVar(&hotInterval, "hot-interval", 100, "Milliseconds at which to poll the provider for updates on the contract while the crawl is hot")
	eventsCmd.Flags().IntVar(&coldInterval, "cold-interval", 10000, "Milliseconds at which to poll the provider for updates on the contract while the crawl is cold")
	eventsCmd.Flags().IntVar(&confirmations, "confirmations", 5, "Number of confirmations to wait for before considering a block canonical")
	eventsCmd.Flags().Uint64Var(&fromBlock, "from", 0, "The block number from which to start crawling")
	eventsCmd.Flags().Uint64Var(&toBlock, "to", 0, "The block number to which to crawl (set to 0 for continuous crawl)")

	return eventsCmd
}

func CreateFindDeploymentCmd() *cobra.Command {
	var providerURL, contractAddress string

	findDeploymentCmd := &cobra.Command{
		Use:   "find-deployment-block",
		Short: "Discover the block number in which a contract was deployed",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if providerURL == "" {
				providerURLFromEnv := os.Getenv("STARKNET_RPC_URL")
				if providerURLFromEnv == "" {
					return errors.New("you must provide a provider URL using -p/--provider or set the STARKNET_RPC_URL environment variable")
				}
				providerURL = providerURLFromEnv
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, clientErr := rpc.NewClient(providerURL)
			if clientErr != nil {
				return clientErr
			}
			provider := rpc.NewProvider(client)
			ctx := context.Background()

			if contractAddress == "" {
				return errors.New("you must provide a contract address using -c/--contract")
			}

			fieldAdditiveIdentity := fp.NewElement(0)
			if contractAddress[:2] == "0x" {
				contractAddress = contractAddress[2:]
			}
			decodedAddress, decodeErr := hex.DecodeString(contractAddress)
			if decodeErr != nil {
				return decodeErr
			}
			address := felt.NewFelt(&fieldAdditiveIdentity)
			address.SetBytes(decodedAddress)

			deploymentBlock, err := DeploymentBlock(ctx, provider, address)
			if err != nil {
				return err
			}

			cmd.Println(deploymentBlock)
			return nil
		},
	}

	findDeploymentCmd.Flags().StringVarP(&providerURL, "provider", "p", "", "The URL of your Starknet RPC provider (defaults to value of STARKNET_RPC_URL environment variable)")
	findDeploymentCmd.Flags().StringVarP(&contractAddress, "contract", "c", "", "The address of the smart contract to find the deployment block for")

	return findDeploymentCmd
}

func CreateParseCommand() *cobra.Command {
	var infile, outfile string

	parseCmd := &cobra.Command{
		Use:   "parse",
		Short: "Parse a file (as produced by the \"stark events\" command) to process previously unknown events",
		RunE: func(cmd *cobra.Command, args []string) error {
			ifp := os.Stdin
			var infileErr error
			if infile != "" && infile != "-" {
				ifp, infileErr = os.Open(infile)
				if infileErr != nil {
					return infileErr
				}
				defer ifp.Close()
			}

			ofp := os.Stdout
			var outfileErr error
			if outfile != "" {
				ofp, outfileErr = os.Create(outfile)
				if outfileErr != nil {
					return outfileErr
				}
				defer ofp.Close()
			}

			parser, newParserErr := NewEventParser()
			if newParserErr != nil {
				return newParserErr
			}

			newline := []byte("\n")

			scanner := bufio.NewScanner(ifp)
			for scanner.Scan() {
				var partialEvent PartialEvent
				line := scanner.Text()
				json.Unmarshal([]byte(line), &partialEvent)

				passThrough := true

				if partialEvent.Name == EVENT_UNKNOWN {
					var event RawEvent
					json.Unmarshal(partialEvent.Event, &event)
					parsedEvent, parseErr := parser.Parse(event)
					if parseErr == nil {
						passThrough = false

						parsedEventBytes, marshalErr := json.Marshal(parsedEvent)
						if marshalErr != nil {
							return marshalErr
						}

						_, writeErr := ofp.Write(parsedEventBytes)
						if writeErr != nil {
							return writeErr
						}
						_, writeErr = ofp.Write(newline)
						if writeErr != nil {
							return writeErr
						}
					}
				}

				if passThrough {
					partialEventBytes, marshalErr := json.Marshal(partialEvent)
					if marshalErr != nil {
						return marshalErr
					}

					_, writeErr := ofp.Write(partialEventBytes)
					if writeErr != nil {
						return writeErr
					}
					_, writeErr = ofp.Write(newline)
					if writeErr != nil {
						return writeErr
					}
				}
			}

			return nil
		},
	}

	parseCmd.Flags().StringVarP(&infile, "infile", "i", "", "File containing crawled events from which to build the leaderboard (as produced by the \"influence-eth stark events\" command, defaults to stdin)")
	parseCmd.Flags().StringVarP(&outfile, "outfile", "o", "", "File to write reparsed events to (defaults to stdout)")

	return parseCmd
}

type LeaderboardCommandCreator func(infile, outfile, accessToken, leaderboardId *string) error

type LeaderboardCommandFunc struct {
	Name        string
	Description string
	Func        LeaderboardCommandCreator
}

var LEADERBOARD_MISSIONS = []LeaderboardCommandFunc{
	{
		Name:        "c-1-base-camp",
		Description: "Prepare community leaderboard",
		Func:        CL1BaseCamp,
	},
	{
		Name:        "c-2-romulus-remus-and-the-rest",
		Description: "Prepare community leaderboard",
		Func:        CL2RomulusRemusAndTheRest,
	},
	{
		Name:        "c-3-learn-by-doing",
		Description: "Prepare community leaderboard",
		Func:        CL3LearnByDoing,
	},
	{
		Name:        "c-4-four-pillars",
		Description: "Prepare community leaderboard",
		Func:        CL4FourPillars,
	},
	{
		Name:        "c-5-together-we-can-rise",
		Description: "Prepare community leaderboard",
		Func:        CL5TogetherWeCanRise,
	},
	{
		Name:        "c-6-the-fleet",
		Description: "Prepare community leaderboard",
		Func:        CL6TheFleet,
	},
	{
		Name:        "c-7-rock-breaker",
		Description: "Prepare community leaderboard",
		Func:        CL7RockBreaker,
	},
	{
		Name:        "c-8-good-news-everyone",
		Description: "Prepare community leaderboard",
		Func:        CL8GoodNewsEveryone,
	},
	{
		Name:        "c-9-prospecting-pays-off",
		Description: "Prepare community leaderboard",
		Func:        CL9ProspectingPaysOff,
	},
	{
		Name:        "c-10-potluck",
		Description: "Prepare community leaderboard",
		Func:        CL10Potluck,
	},
	{
		Name:        "1-new-recruits-r1",
		Description: "Prepare leaderboard",
		Func:        L1NewRecruitsR1,
	},
	{
		Name:        "1-new-recruits-r2",
		Description: "Prepare leaderboard",
		Func:        L1NewRecruitsR2,
	},
	{
		Name:        "2-buried-treasure-r1",
		Description: "Prepare leaderboard",
		Func:        L2BuriedTreasureR1,
	},
	{
		Name:        "2-buried-treasure-r2",
		Description: "Prepare leaderboard",
		Func:        L2BuriedTreasureR2,
	},
	{
		Name:        "3-market-maker-r1",
		Description: "Prepare leaderboard",
		Func:        L3MarketMakerR1,
	},
	{
		Name:        "3-market-maker-r2",
		Description: "Prepare leaderboard",
		Func:        L3MarketMakerR2,
	},
	{
		Name:        "4-breaking-ground-r1",
		Description: "Prepare leaderboard",
		Func:        L4BreakingGroundR1,
	},
	{
		Name:        "4-breaking-ground-r2",
		Description: "Prepare leaderboard",
		Func:        L4BreakingGroundR2,
	},
	{
		Name:        "5-city-builder",
		Description: "Prepare leaderboard",
		Func:        L5CityBuilder,
	},
	{
		Name:        "6-explore-the-stars-r1",
		Description: "Prepare leaderboard",
		Func:        L6ExploreTheStarsR1,
	},
	{
		Name:        "6-explore-the-stars-r2",
		Description: "Prepare leaderboard",
		Func:        L6ExploreTheStarsR2,
	},
	{
		Name:        "7-expand-the-colony",
		Description: "Prepare leaderboard",
		Func:        L7ExpandTheColony,
	},
	{
		Name:        "8-special-delivery",
		Description: "Prepare leaderboard",
		Func:        L8SpecialDelivery,
	},
	{
		Name:        "9-dinner-is-served",
		Description: "Prepare leaderboard",
		Func:        L9DinnerIsServed,
	},
}

type LeaderboardsMap struct {
	Name          string `json:"name"`
	LeaderboardId string `json:"leaderboard_id"`
}

func CreateLeaderboardsCommand() *cobra.Command {
	var infile, accessToken, leaderboardsMapFilePath string

	leaderboardsCmd := &cobra.Command{
		Use:   "leaderboards",
		Short: "Prepare all Moonstream.to leaderboards",
		RunE: func(cmd *cobra.Command, args []string) error {
			var inputFile *os.File
			var readErr error
			if leaderboardsMapFilePath != "" {
				inputFile, readErr = os.Open(leaderboardsMapFilePath)
				if readErr != nil {
					log.Fatalf("Unable to read file %s, err: %v", leaderboardsMapFilePath, readErr)
				}
			} else {
				log.Fatalf("Please specify file with events with --input flag")
			}

			defer inputFile.Close()

			byteValue, err := ioutil.ReadAll(inputFile)
			if err != nil {
				log.Fatalf("Error reading file, err: %v", err)
			}

			leaderboardsMap := make(map[string]string)
			err = json.Unmarshal(byteValue, &leaderboardsMap)
			if err != nil {
				log.Fatalf("Error unmarshalling JSON, err: %v", err)
			}

			for _, lm := range LEADERBOARD_MISSIONS {
				lId, ok := leaderboardsMap[lm.Name]
				if !ok {
					log.Printf("Passed %s leaderboard, not ID passed in config file", lm.Name)
					continue
				}
				emptyOutput := ""
				err := lm.Func(&infile, &emptyOutput, &accessToken, &lId)
				if err != nil {
					log.Printf("Failed %s leaderboard", lm.Name)
					continue
				}

				log.Printf("Updated %s leaderboard known as %s", lId, lm.Name)
				time.Sleep(500 * time.Millisecond)
			}

			return nil
		},
	}

	leaderboardsCmd.PersistentFlags().StringVarP(&infile, "infile", "i", "", "File containing crawled events from which to build the leaderboard (as produced by the \"influence-eth stark events\" command, defaults to stdin)")
	leaderboardsCmd.PersistentFlags().StringVarP(&accessToken, "token", "t", "", "Moonstream user access token (could be set with MOONSTREAM_ACCESS_TOKEN environment variable)")
	leaderboardsCmd.PersistentFlags().StringVarP(&leaderboardsMapFilePath, "leaderboards-map", "m", "", "Pass to leaderboards map JSON file")

	return leaderboardsCmd
}

func CreateLeaderboardCommand() *cobra.Command {
	var infile, outfile, accessToken, leaderboardId string

	leaderboardCmd := &cobra.Command{
		Use:   "leaderboard",
		Short: "Prepare Moonstream.to leaderboard",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
		},
	}

	leaderboardCmd.PersistentFlags().StringVarP(&infile, "infile", "i", "", "File containing crawled events from which to build the leaderboard (as produced by the \"influence-eth stark events\" command, defaults to stdin)")
	leaderboardCmd.PersistentFlags().StringVarP(&outfile, "outfile", "o", "", "File to write reparsed events to (defaults to stdout)")
	leaderboardCmd.PersistentFlags().StringVarP(&accessToken, "token", "t", "", "Moonstream user access token (could be set with MOONSTREAM_ACCESS_TOKEN environment variable)")
	leaderboardCmd.PersistentFlags().StringVarP(&leaderboardId, "leaderboard-id", "l", "", "Leaderboard ID to update data for at Moonstream.to portal")

	for _, lm := range LEADERBOARD_MISSIONS {
		lm := lm // Create a local copy of lm for closure to capture
		newCmd := &cobra.Command{
			Use:   lm.Name,
			Short: lm.Description,
			RunE: func(cmd *cobra.Command, args []string) error {
				err := lm.Func(&infile, &outfile, &accessToken, &leaderboardId)
				return err
			},
		}
		leaderboardCmd.AddCommand(newCmd)
	}

	lCrewOwnersCmd := CreateLCrewOwnersCommand(&infile, &outfile, &accessToken, &leaderboardId)
	lCrewsCmd := CreateLCrewsCommand(&infile, &outfile, &accessToken, &leaderboardId)

	leaderboardCmd.AddCommand(lCrewOwnersCmd, lCrewsCmd)

	return leaderboardCmd
}

func CL1BaseCamp(infile, outfile, accessToken, leaderboardId *string) error {
	events, parseEventsErr := ParseEventFromFile[TransitFinished](*infile, "TransitFinished")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	scores := GenerateC1BaseCampToScores(events)

	outErr := PrepareLeaderboardOutput(scores, *outfile, *accessToken, *leaderboardId)
	if outErr != nil {
		return outErr
	}

	return nil
}

func CL2RomulusRemusAndTheRest(infile, outfile, accessToken, leaderboardId *string) error {
	conPlanEvents, parseEventsErr := ParseEventFromFile[ConstructionPlanned](*infile, "ConstructionPlanned")
	if parseEventsErr != nil {
		return parseEventsErr
	}
	conFinEvents, parseEventsErr := ParseEventFromFile[ConstructionFinished](*infile, "ConstructionFinished")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	asteroids := map[uint64]bool{
		1: true, // AP
	}
	scores := GenerateCommunityConstructionsToScores(conPlanEvents, conFinEvents, nil, asteroids, 75000)

	outErr := PrepareLeaderboardOutput(scores, *outfile, *accessToken, *leaderboardId)
	if outErr != nil {
		return outErr
	}

	return nil
}

func CL3LearnByDoing(infile, outfile, accessToken, leaderboardId *string) error {
	conPlanEvents, parseEventsErr := ParseEventFromFile[ConstructionPlanned](*infile, "ConstructionPlanned")
	if parseEventsErr != nil {
		return parseEventsErr
	}
	conFinEvents, parseEventsErr := ParseEventFromFile[ConstructionFinished](*infile, "ConstructionFinished")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	buildingTypes := map[uint64]bool{
		1: true, // Warehouse
		2: true, // Extractor
	}
	scores := GenerateCommunityConstructionsToScores(conPlanEvents, conFinEvents, buildingTypes, nil, 30000)

	outErr := PrepareLeaderboardOutput(scores, *outfile, *accessToken, *leaderboardId)
	if outErr != nil {
		return outErr
	}

	return nil
}

func CL4FourPillars(infile, outfile, accessToken, leaderboardId *string) error {
	conPlanEvents, parseEventsErr := ParseEventFromFile[ConstructionPlanned](*infile, "ConstructionPlanned")
	if parseEventsErr != nil {
		return parseEventsErr
	}
	conFinEvents, parseEventsErr := ParseEventFromFile[ConstructionFinished](*infile, "ConstructionFinished")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	buildingTypes := map[uint64]bool{
		3: true, // Refinery
		4: true, // Bioreactor
		5: true, // Factory
		6: true, // Shipyard
	}
	scores := GenerateCommunityConstructionsToScores(conPlanEvents, conFinEvents, buildingTypes, nil, 15000)

	outErr := PrepareLeaderboardOutput(scores, *outfile, *accessToken, *leaderboardId)
	if outErr != nil {
		return outErr
	}

	return nil
}

func CL5TogetherWeCanRise(infile, outfile, accessToken, leaderboardId *string) error {
	conPlanEvents, parseEventsErr := ParseEventFromFile[ConstructionPlanned](*infile, "ConstructionPlanned")
	if parseEventsErr != nil {
		return parseEventsErr
	}
	conFinEvents, parseEventsErr := ParseEventFromFile[ConstructionFinished](*infile, "ConstructionFinished")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	buildingTypes := map[uint64]bool{
		7: true, // Spaceport
		8: true, // Marketplace
		9: true, // Habitat
	}
	scores := GenerateCommunityConstructionsToScores(conPlanEvents, conFinEvents, buildingTypes, nil, 1000)

	outErr := PrepareLeaderboardOutput(scores, *outfile, *accessToken, *leaderboardId)
	if outErr != nil {
		return outErr
	}

	return nil
}

func CL6TheFleet(infile, outfile, accessToken, leaderboardId *string) error {
	events, parseEventsErr := ParseEventFromFile[ShipAssemblyFinished](*infile, "ShipAssemblyFinished")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	scores := GenerateC6TheFleet(events)

	outErr := PrepareLeaderboardOutput(scores, *outfile, *accessToken, *leaderboardId)
	if outErr != nil {
		return outErr
	}

	return nil
}

func CL7RockBreaker(infile, outfile, accessToken, leaderboardId *string) error {
	events, parseEventsErr := ParseEventFromFile[ResourceExtractionFinished](*infile, "ResourceExtractionFinished")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	scores := GenerateC7RockBreaker(events)

	outErr := PrepareLeaderboardOutput(scores, *outfile, *accessToken, *leaderboardId)
	if outErr != nil {
		return outErr
	}

	return nil
}

func CL8GoodNewsEveryone(infile, outfile, accessToken, leaderboardId *string) error {
	unknownEvents, parseEventsErr := ParseEventFromFile[RawEvent](*infile, "UNKNOWN")
	if parseEventsErr != nil {
		return parseEventsErr
	}
	trFinEvents, parseEventsErr := ParseEventFromFile[TransitFinished](*infile, "TransitFinished")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	scores := GenerateC8GoodNewsEveryoneToScores(trFinEvents, unknownEvents)

	outErr := PrepareLeaderboardOutput(scores, *outfile, *accessToken, *leaderboardId)
	if outErr != nil {
		return outErr
	}

	return nil
}

func CL9ProspectingPaysOff(infile, outfile, accessToken, leaderboardId *string) error {
	events, parseEventsErr := ParseEventFromFile[SamplingDepositFinished](*infile, "SamplingDepositFinished")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	scores := GenerateC9ProspectingPaysOff(events)

	outErr := PrepareLeaderboardOutput(scores, *outfile, *accessToken, *leaderboardId)
	if outErr != nil {
		return outErr
	}

	return nil
}

func CL10Potluck(infile, outfile, accessToken, leaderboardId *string) error {
	stEventsV1, parseEventsErr := ParseEventFromFile[MaterialProcessingStartedV1](*infile, "MaterialProcessingStartedV1")
	if parseEventsErr != nil {
		return parseEventsErr
	}
	finEvents, parseEventsErr := ParseEventFromFile[MaterialProcessingFinished](*infile, "MaterialProcessingFinished")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	scores := GenerateC10Potluck(stEventsV1, finEvents)

	outErr := PrepareLeaderboardOutput(scores, *outfile, *accessToken, *leaderboardId)
	if outErr != nil {
		return outErr
	}

	return nil
}

func CreateLCrewOwnersCommand(infile, outfile, accessToken, leaderboardId *string) *cobra.Command {
	leaderboardCrewOwnersCmd := &cobra.Command{
		Use:   "crew-owners",
		Short: "Prepare leaderboard with crews",
		RunE: func(cmd *cobra.Command, args []string) error {
			events, parseEventsErr := ParseEventFromFile[Influence_Contracts_Crew_Crew_Transfer](*infile, "influence::contracts::crew::Crew::Transfer")
			if parseEventsErr != nil {
				return parseEventsErr
			}

			scores := GenerateCrewOwnersToScores(events)

			outErr := PrepareLeaderboardOutput(scores, *outfile, *accessToken, *leaderboardId)
			if outErr != nil {
				return outErr
			}

			return nil
		},
	}

	return leaderboardCrewOwnersCmd
}

func CreateLCrewsCommand(infile, outfile, accessToken, leaderboardId *string) *cobra.Command {
	leaderboardCrewsCmd := &cobra.Command{
		Use:   "crews",
		Short: "Prepare leaderboard with crews",
		RunE: func(cmd *cobra.Command, args []string) error {
			events, parseEventsErr := ParseEventFromFile[Influence_Contracts_Crew_Crew_Transfer](*infile, "influence::contracts::crew::Crew::Transfer")
			if parseEventsErr != nil {
				return parseEventsErr
			}

			scores := GenerateOwnerCrewsToScores(events)

			outErr := PrepareLeaderboardOutput(scores, *outfile, *accessToken, *leaderboardId)
			if outErr != nil {
				return outErr
			}

			return nil
		},
	}

	return leaderboardCrewsCmd
}

func L1NewRecruitsR1(infile, outfile, accessToken, leaderboardId *string) error {
	recEvents, parseEventsErr := ParseEventFromFile[CrewmateRecruited](*infile, "CrewmateRecruited")
	if parseEventsErr != nil {
		return parseEventsErr
	}
	recV1Events, parseEventsErr := ParseEventFromFile[CrewmateRecruitedV1](*infile, "CrewmateRecruitedV1")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	scores := Generate1NewRecruitsR1(recEvents, recV1Events)

	outErr := PrepareLeaderboardOutput(scores, *outfile, *accessToken, *leaderboardId)
	if outErr != nil {
		return outErr
	}

	return nil
}

func L1NewRecruitsR2(infile, outfile, accessToken, leaderboardId *string) error {
	recEvents, parseEventsErr := ParseEventFromFile[CrewmateRecruited](*infile, "CrewmateRecruited")
	if parseEventsErr != nil {
		return parseEventsErr
	}
	recV1Events, parseEventsErr := ParseEventFromFile[CrewmateRecruitedV1](*infile, "CrewmateRecruitedV1")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	scores := Generate1NewRecruitsR2(recEvents, recV1Events)

	outErr := PrepareLeaderboardOutput(scores, *outfile, *accessToken, *leaderboardId)
	if outErr != nil {
		return outErr
	}

	return nil
}

func L2BuriedTreasureR1(infile, outfile, accessToken, leaderboardId *string) error {
	stEventsV1, parseEventsErr := ParseEventFromFile[MaterialProcessingStartedV1](*infile, "MaterialProcessingStartedV1")
	if parseEventsErr != nil {
		return parseEventsErr
	}
	finEvents, parseEventsErr := ParseEventFromFile[MaterialProcessingFinished](*infile, "MaterialProcessingFinished")
	if parseEventsErr != nil {
		return parseEventsErr
	}
	sofEvents, parseEventsErr := ParseEventFromFile[SellOrderFilled](*infile, "SellOrderFilled")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	scores := Generate2BuriedTreasureR1(stEventsV1, finEvents, sofEvents)

	outErr := PrepareLeaderboardOutput(scores, *outfile, *accessToken, *leaderboardId)
	if outErr != nil {
		return outErr
	}

	return nil
}

func L2BuriedTreasureR2(infile, outfile, accessToken, leaderboardId *string) error {
	sdsEvents, parseEventsErr := ParseEventFromFile[SamplingDepositStarted](*infile, "SamplingDepositStarted")
	if parseEventsErr != nil {
		return parseEventsErr
	}
	sdsEventsV1, parseEventsErr := ParseEventFromFile[SamplingDepositStartedV1](*infile, "SamplingDepositStartedV1")
	if parseEventsErr != nil {
		return parseEventsErr
	}
	sdfEvents, parseEventsErr := ParseEventFromFile[SamplingDepositFinished](*infile, "SamplingDepositFinished")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	scores := Generate2BuriedTreasureR2(sdsEvents, sdsEventsV1, sdfEvents)

	outErr := PrepareLeaderboardOutput(scores, *outfile, *accessToken, *leaderboardId)
	if outErr != nil {
		return outErr
	}

	return nil
}

func L3MarketMakerR1(infile, outfile, accessToken, leaderboardId *string) error {
	buyEvents, parseEventsErr := ParseEventFromFile[BuyOrderFilled](*infile, "BuyOrderFilled")
	if parseEventsErr != nil {
		return parseEventsErr
	}
	sellEvents, parseEventsErr := ParseEventFromFile[SellOrderFilled](*infile, "SellOrderFilled")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	scores := Generate3MarketMakerR1(buyEvents, sellEvents)

	outErr := PrepareLeaderboardOutput(scores, *outfile, *accessToken, *leaderboardId)
	if outErr != nil {
		return outErr
	}

	return nil
}

func L3MarketMakerR2(infile, outfile, accessToken, leaderboardId *string) error {
	buyEvents, parseEventsErr := ParseEventFromFile[BuyOrderCreated](*infile, "BuyOrderCreated")
	if parseEventsErr != nil {
		return parseEventsErr
	}
	sellEvents, parseEventsErr := ParseEventFromFile[SellOrderCreated](*infile, "SellOrderCreated")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	scores := Generate3MarketMakerR2(buyEvents, sellEvents)

	outErr := PrepareLeaderboardOutput(scores, *outfile, *accessToken, *leaderboardId)
	if outErr != nil {
		return outErr
	}

	return nil
}

func L4BreakingGroundR1(infile, outfile, accessToken, leaderboardId *string) error {
	events, parseEventsErr := ParseEventFromFile[ResourceExtractionFinished](*infile, "ResourceExtractionFinished")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	scores := Generate4BreakingGroundR1(events)

	outErr := PrepareLeaderboardOutput(scores, *outfile, *accessToken, *leaderboardId)
	if outErr != nil {
		return outErr
	}

	return nil
}

func L4BreakingGroundR2(infile, outfile, accessToken, leaderboardId *string) error {
	events, parseEventsErr := ParseEventFromFile[ResourceExtractionFinished](*infile, "ResourceExtractionFinished")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	scores := Generate4BreakingGroundR2(events)

	outErr := PrepareLeaderboardOutput(scores, *outfile, *accessToken, *leaderboardId)
	if outErr != nil {
		return outErr
	}

	return nil
}

func L5CityBuilder(infile, outfile, accessToken, leaderboardId *string) error {
	conFinEvents, parseEventsErr := ParseEventFromFile[ConstructionFinished](*infile, "ConstructionFinished")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	conPlanEvents, parseEventsErr := ParseEventFromFile[ConstructionPlanned](*infile, "ConstructionPlanned")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	scores := Generate5CityBuilder(conFinEvents, conPlanEvents)

	outErr := PrepareLeaderboardOutput(scores, *outfile, *accessToken, *leaderboardId)
	if outErr != nil {
		return outErr
	}

	return nil
}

func L6ExploreTheStarsR1(infile, outfile, accessToken, leaderboardId *string) error {
	events, parseEventsErr := ParseEventFromFile[ShipAssemblyFinished](*infile, "ShipAssemblyFinished")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	scores := Generate6ExploreTheStarsR1(events)

	outErr := PrepareLeaderboardOutput(scores, *outfile, *accessToken, *leaderboardId)
	if outErr != nil {
		return outErr
	}

	return nil
}

func L6ExploreTheStarsR2(infile, outfile, accessToken, leaderboardId *string) error {
	events, parseEventsErr := ParseEventFromFile[TransitFinished](*infile, "TransitFinished")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	scores := Generate6ExploreTheStarsR2(events)

	outErr := PrepareLeaderboardOutput(scores, *outfile, *accessToken, *leaderboardId)
	if outErr != nil {
		return outErr
	}

	return nil
}

func L7ExpandTheColony(infile, outfile, accessToken, leaderboardId *string) error {
	conFinEvents, parseEventsErr := ParseEventFromFile[ConstructionFinished](*infile, "ConstructionFinished")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	conPlanEvents, parseEventsErr := ParseEventFromFile[ConstructionPlanned](*infile, "ConstructionPlanned")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	scores := Generate7ExpandTheColony(conFinEvents, conPlanEvents)

	outErr := PrepareLeaderboardOutput(scores, *outfile, *accessToken, *leaderboardId)
	if outErr != nil {
		return outErr
	}

	return nil
}

func L8SpecialDelivery(infile, outfile, accessToken, leaderboardId *string) error {
	unknownEvents, parseEventsErr := ParseEventFromFile[RawEvent](*infile, "UNKNOWN")
	if parseEventsErr != nil {
		return parseEventsErr
	}
	trEvents, parseEventsErr := ParseEventFromFile[TransitFinished](*infile, "TransitFinished")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	scores := Generate8SpecialDelivery(trEvents, unknownEvents)

	outErr := PrepareLeaderboardOutput(scores, *outfile, *accessToken, *leaderboardId)
	if outErr != nil {
		return outErr
	}

	return nil
}

func L9DinnerIsServed(infile, outfile, accessToken, leaderboardId *string) error {
	events, parseEventsErr := ParseEventFromFile[FoodSupplied](*infile, "FoodSupplied")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	eventsV1, parseEventsErr := ParseEventFromFile[FoodSuppliedV1](*infile, "FoodSuppliedV1")
	if parseEventsErr != nil {
		return parseEventsErr
	}

	scores := Generate9DinnerIsServed(events, eventsV1)

	outErr := PrepareLeaderboardOutput(scores, *outfile, *accessToken, *leaderboardId)
	if outErr != nil {
		return outErr
	}

	return nil
}
