package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"sort"
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
