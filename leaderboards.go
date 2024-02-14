package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

var (
	MOONSTREAM_API_URL = os.Getenv("MOONSTREAM_API_URL")
)

type LeaderboardScore struct {
	Address    string      `json:"address"`
	Score      int         `json:"score"`
	PointsData interface{} `json:"points_data"`
}

type TokenKey struct {
	Str    string
	BigInt *big.Int
}

func ParseEventFromFile[T any](filePath, expectedEventName string) ([]T, error) {
	var inputFile *os.File
	var readErr error

	if filePath != "" {
		inputFile, readErr = os.Open(filePath)
		if readErr != nil {
			return nil, fmt.Errorf("Unable to read file %s, err: %v", filePath, readErr)
		}
	} else {
		return nil, fmt.Errorf("Please specify file with events with --input flag")
	}

	defer inputFile.Close()

	var events []T

	scanner := bufio.NewScanner(inputFile)
	for scanner.Scan() {
		var line PartialEvent
		unmErr := json.Unmarshal(scanner.Bytes(), &line)
		if unmErr != nil {
			log.Printf("Error parsing JSON line: %v", unmErr)
			continue
		}

		if line.Name != expectedEventName {
			continue
		}

		var event T
		unmEventErr := json.Unmarshal(line.Event, &event)
		if unmEventErr != nil {
			log.Printf("Error parsing Event: %v", unmErr)
			continue
		}

		events = append(events, event)
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return nil, fmt.Errorf("Error reading file: %v", scanErr)
	}

	return events, nil
}

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

func PrepareLeaderboardOutput(scores []LeaderboardScore, outfile, accessToken, leaderboardId string) {
	jsonData, marshErr := json.Marshal(scores)
	if marshErr != nil {
		log.Fatalf("Error marshaling scores: %v", marshErr)
	}

	if outfile != "" {
		writeErr := os.WriteFile(outfile, jsonData, 0644)
		if writeErr != nil {
			log.Fatalf("Error writing to file: %v", marshErr)
		}
	}

	accessTokenEnv := os.Getenv("MOONSTREAM_ACCESS_TOKEN")
	if accessTokenEnv != "" {
		accessToken = accessTokenEnv
	}

	if leaderboardId != "" && accessToken != "" {
		statusCode, reqErr := UpdateLeaderboardScores(accessToken, leaderboardId, bytes.NewBuffer(jsonData))
		if reqErr != nil {
			log.Fatal(reqErr)
		}
		fmt.Printf("Status code: %d\n", statusCode)
	}
}

func FindAndDelete(original []*big.Int, delItem *big.Int) []*big.Int {
	idx := 0
	for _, val := range original {
		if val.Cmp(delItem) != 0 {
			original[idx] = val
			idx++
		}
	}
	return original[:idx]
}

func GenerateCrewOwnersToScores(events []Influence_Contracts_Crew_Crew_Transfer) []LeaderboardScore {
	// Prepare crew owners map in format (390: 0x123)
	crewOwners := make(map[string]string)
	crewOwnerKeys := []TokenKey{}

	for _, event := range events {
		tokenIdStr := event.TokenId.String()

		if event.To != "0x0" {
			delete(crewOwners, tokenIdStr)
		}
		crewOwners[tokenIdStr] = event.To

		is_found := false
		for _, tk := range crewOwnerKeys {
			if tk.Str == tokenIdStr {
				is_found = true
				break
			}
		}
		if !is_found {
			crewOwnerKeys = append(crewOwnerKeys, TokenKey{Str: tokenIdStr, BigInt: event.TokenId})
		}
	}

	sort.Slice(crewOwnerKeys, func(i, j int) bool {
		return crewOwnerKeys[i].BigInt.Cmp(crewOwnerKeys[j].BigInt) < 0
	})

	scores := []LeaderboardScore{}
	for i, k := range crewOwnerKeys {
		scores = append(scores, LeaderboardScore{
			Address: k.Str,
			Score:   i + 1,
			PointsData: map[string]string{
				"address": crewOwners[k.Str],
			},
		})
	}

	return scores
}

func GenerateOwnerCrewsToScores(events []Influence_Contracts_Crew_Crew_Transfer) []LeaderboardScore {
	// Prepare owner crews map in format (0x123: [390, 428])
	ownerCrews := make(map[string][]*big.Int)
	for _, event := range events {
		if vals, ok := ownerCrews[event.To]; ok {
			ownerCrews[event.To] = append(vals, event.TokenId)
			if event.From != "0x0" {
				ownerCrews[event.From] = FindAndDelete(ownerCrews[event.From], event.TokenId)
			}
		} else {
			ownerCrews[event.To] = []*big.Int{event.TokenId}
			if event.From != "0x0" {
				ownerCrews[event.From] = FindAndDelete(ownerCrews[event.From], event.TokenId)
			}
		}
	}

	scores := []LeaderboardScore{}
	for owner, crews := range ownerCrews {
		scores = append(scores, LeaderboardScore{
			Address: owner,
			Score:   len(crews),
			PointsData: map[string][]*big.Int{
				"crews": crews,
			},
		})
	}

	return scores
}

type ShipAssemblyFinishedScore struct {
	Caller      string
	FinishTime  uint64
	Destination Influence_Common_Types_Entity_Entity
	Ship        Influence_Common_Types_Entity_Entity
}

func GenerateShipAssemblyFinished(events []ShipAssemblyFinished) []LeaderboardScore {
	/*
		{"Name":"ShipAssemblyFinished","Event":{"Ship":{"Label":6,"Id":6},"DryDock":{"Label":5,"Id":57},"DryDockSlot":1,"Destination":{"Label":5,"Id":124},"FinishTime":1707753830,"CallerCrew":{"Label":1,"Id":39},"Caller":"0x24876d7edd78740466d3f80ec4cba1a43a72bbf201aec37b66e2019ea31b64d"}}
	*/

	byCrews := make(map[uint64][]ShipAssemblyFinishedScore, len(events))
	for _, event := range events {
		if _, ok := byCrews[event.CallerCrew.Id]; !ok {
			byCrews[event.CallerCrew.Id] = []ShipAssemblyFinishedScore{}
		}
		byCrews[event.CallerCrew.Id] = append(byCrews[event.CallerCrew.Id], ShipAssemblyFinishedScore{Caller: event.Caller,
			FinishTime:  event.FinishTime,
			Destination: event.Destination,
			Ship:        event.Ship})
	}

	scores := []LeaderboardScore{}
	for crew, data := range byCrews {
		scores = append(scores, LeaderboardScore{
			Address: fmt.Sprintf("%d", crew),
			Score:   len(data),
			PointsData: map[string][]ShipAssemblyFinishedScore{
				"ships": data,
			},
		})
	}

	return scores
}
