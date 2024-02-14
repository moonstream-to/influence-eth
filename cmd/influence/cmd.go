package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/consensys/gnark-crypto/ecc/stark-curve/fp"
	"github.com/spf13/cobra"

	crew "github.com/moonstream-to/influence-eth/influence/crew"
)

var (
	MOONSTREAM_API_URL = os.Getenv("MOONSTREAM_API_URL")
)

func UpdateLeaderboardScores(accessToken, leaderboardId string, body io.Reader) (int, error) {
	if MOONSTREAM_API_URL != "" {
		MOONSTREAM_API_URL = strings.TrimRight(MOONSTREAM_API_URL, "/")
	} else {
		MOONSTREAM_API_URL = "https://engineapi.moonstream.to"
	}

	request, requestErr := http.NewRequest("PUT", fmt.Sprintf("%s/leaderboard/%s/scores?normalize_addresses=false&overwrite=true", MOONSTREAM_API_URL, leaderboardId), body)
	if requestErr != nil {
		return 0, fmt.Errorf("error making requests: %v", requestErr)
	}

	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	request.Header.Add("Accept", "application/json")
	request.Header.Add("Content-Type", "application/json")

	timeout := time.Duration(10) * time.Second
	httpClient := http.Client{Timeout: timeout}
	response, responseErr := httpClient.Do(request)
	if responseErr != nil {
		return 0, fmt.Errorf("error parsing response: %v", responseErr)
	}
	defer response.Body.Close()

	return response.StatusCode, nil

}

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
	rootCmd.AddCommand(completionCmd, versionCmd, eventsCmd, findDeploymentBlockCmd, parseCmd, leaderboardCmd)

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

func ParseCrewTransferEvents(infile string) (*[]crew.Influence_Contracts_Crew_Crew_Transfer, error) {
	var inputFile *os.File
	var readErr error

	if infile != "" {
		inputFile, readErr = os.Open(infile)
		if readErr != nil {
			return nil, fmt.Errorf("Unable to read file %s, err: %v", infile, readErr)
		}
	} else {
		return nil, fmt.Errorf("Please specify file with events with --input flag")
	}

	defer inputFile.Close()

	var events []crew.Influence_Contracts_Crew_Crew_Transfer

	scanner := bufio.NewScanner(inputFile)
	for scanner.Scan() {
		var line PartialEvent
		unmErr := json.Unmarshal(scanner.Bytes(), &line)
		if unmErr != nil {
			log.Printf("Error parsing JSON line: %v", unmErr)
			continue
		}

		var event crew.Influence_Contracts_Crew_Crew_Transfer
		unmEventErr := json.Unmarshal(line.Event, &event)
		if unmEventErr != nil {
			log.Printf("Error parsing Event: %v", unmErr)
			continue
		}

		if event.TokenId == nil {
			continue
		}

		events = append(events, event)
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return nil, fmt.Errorf("Error reading file: %v", scanErr)
	}

	return &events, nil
}

func CreateLeaderboardCommand() *cobra.Command {
	leaderboardCmd := &cobra.Command{
		Use:   "leaderboard",
		Short: "Prepare Moonstream.to leaderboards",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
		},
	}

	leaderboardCrewOwnersCmd := CreateLeaderboardCrewOwnersCommand()
	leaderboardCrewsCmd := CreateLeaderboardCrewsCommand()
	leaderboardCmd.AddCommand(leaderboardCrewOwnersCmd, leaderboardCrewsCmd)

	return leaderboardCmd
}

func CreateLeaderboardCrewOwnersCommand() *cobra.Command {
	var infile, outfile, accessToken, leaderboardId string

	leaderboardCrewOwnersCmd := &cobra.Command{
		Use:   "crew-owners",
		Short: "Prepare leaderboard with crews",
		RunE: func(cmd *cobra.Command, args []string) error {
			events, parseEventsErr := ParseCrewTransferEvents(infile)
			if parseEventsErr != nil {
				return parseEventsErr
			}

			scores := GenerateCrewOwnersToScores(*events)

			PrepareLeaderboardOutput(scores, outfile, accessToken, leaderboardId)

			return nil
		},
	}

	leaderboardCrewOwnersCmd.Flags().StringVarP(&infile, "infile", "i", "", "File containing crawled events from which to build the leaderboard (as produced by the \"influence-eth stark events\" command, defaults to stdin)")
	leaderboardCrewOwnersCmd.Flags().StringVarP(&outfile, "outfile", "o", "", "File to write reparsed events to (defaults to stdout)")
	leaderboardCrewOwnersCmd.Flags().StringVarP(&accessToken, "token", "t", "", "Moonstream user access token (could be set with MOONSTREAM_ACCESS_TOKEN environment variable)")
	leaderboardCrewOwnersCmd.Flags().StringVarP(&leaderboardId, "leaderboard-id", "l", "", "Leaderboard ID to update data for at Moonstream.to portal")

	return leaderboardCrewOwnersCmd
}

func CreateLeaderboardCrewsCommand() *cobra.Command {
	var infile, outfile, accessToken, leaderboardId string

	leaderboardCrewsCmd := &cobra.Command{
		Use:   "crews",
		Short: "Prepare leaderboard with crews",
		RunE: func(cmd *cobra.Command, args []string) error {
			events, parseEventsErr := ParseCrewTransferEvents(infile)
			if parseEventsErr != nil {
				return parseEventsErr
			}

			scores := GenerateOwnerCrewsToScores(*events)

			PrepareLeaderboardOutput(scores, outfile, accessToken, leaderboardId)

			return nil
		},
	}

	leaderboardCrewsCmd.Flags().StringVarP(&infile, "infile", "i", "", "File containing crawled events from which to build the leaderboard (as produced by the \"influence-eth stark events\" command, defaults to stdin)")
	leaderboardCrewsCmd.Flags().StringVarP(&outfile, "outfile", "o", "", "File to write reparsed events to (defaults to stdout)")
	leaderboardCrewsCmd.Flags().StringVarP(&accessToken, "token", "t", "", "Moonstream user access token (could be set with MOONSTREAM_ACCESS_TOKEN environment variable)")
	leaderboardCrewsCmd.Flags().StringVarP(&leaderboardId, "leaderboard-id", "l", "", "Leaderboard ID to update data for at Moonstream.to portal")

	return leaderboardCrewsCmd
}
