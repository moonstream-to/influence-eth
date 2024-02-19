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
	Score      uint64      `json:"score"`
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

func PrepareLeaderboardOutput(scores []LeaderboardScore, outfile, accessToken, leaderboardId string) error {
	jsonData, marshErr := json.Marshal(scores)
	if marshErr != nil {
		return fmt.Errorf("Error marshaling scores: %v", marshErr)
	}

	if outfile != "" {
		writeErr := os.WriteFile(outfile, jsonData, 0644)
		if writeErr != nil {
			return fmt.Errorf("Error writing to file: %v", marshErr)
		}
	}

	accessTokenEnv := os.Getenv("MOONSTREAM_ACCESS_TOKEN")
	if accessTokenEnv != "" {
		accessToken = accessTokenEnv
	}

	if leaderboardId != "" && accessToken != "" {
		_, reqErr := UpdateLeaderboardScores(accessToken, leaderboardId, bytes.NewBuffer(jsonData))
		if reqErr != nil {
			return reqErr
		}

	}
	return nil
}

func FindAndDeleteBigInt(original []*big.Int, delItem *big.Int) []*big.Int {
	idx := 0
	for _, val := range original {
		if val.Cmp(delItem) != 0 {
			original[idx] = val
			idx++
		}
	}
	return original[:idx]
}

type StationedScore struct {
	Building     Influence_Common_Types_Entity_Entity
	BuildingType uint64
	Asteroid     Influence_Common_Types_Entity_Entity
	Station      Influence_Common_Types_Entity_Entity
}

func GenerateC1BaseCampToScores(staEvents []CrewStationed, conPlanEvents []ConstructionPlanned) []LeaderboardScore {
	buildingType := uint64(9) // Habitat - TODO: station should contains Habitat?
	asteroidAPId := uint64(1)

	byCrews := make(map[uint64][]StationedScore)
	for _, se := range staEvents {
		var stationedScore *StationedScore
		for _, cpe := range conPlanEvents {
			if cpe.BlockNumber > se.BlockNumber {
				continue
			}
			if cpe.Asteroid.Id == asteroidAPId {
				continue
			}
			if se.CallerCrew.Id != cpe.CallerCrew.Id {
				continue
			}
			if cpe.BuildingType != buildingType {
				continue
			}
			stationedScore = &StationedScore{
				Building:     cpe.Building,
				BuildingType: cpe.BuildingType,
				Station:      se.Station,
				Asteroid:     cpe.Asteroid,
			}
		}
		if stationedScore == nil {
			continue
		}
		if _, ok := byCrews[se.CallerCrew.Id]; !ok {
			byCrews[se.CallerCrew.Id] = []StationedScore{}
		}
		byCrews[se.CallerCrew.Id] = append(byCrews[se.CallerCrew.Id], *stationedScore)
	}

	scores := []LeaderboardScore{}
	for crew, data := range byCrews {
		isRequirementComplete := false
		isMustReachComplete := false
		if len(data) >= 1 {
			isRequirementComplete = true
		}
		if len(data) >= 10 {
			isMustReachComplete = true
		}
		scores = append(scores, LeaderboardScore{
			Address: fmt.Sprintf("%d", crew),
			Score:   uint64(len(data)),
			PointsData: map[string]any{
				"complete":          isRequirementComplete,
				"mustReachComplete": isMustReachComplete,
				"data":              data,
			},
		})
	}
	return scores
}

type ConstructionScore struct {
	CallerCrew   Influence_Common_Types_Entity_Entity
	Asteroid     Influence_Common_Types_Entity_Entity
	Building     Influence_Common_Types_Entity_Entity
	BuildingType uint64
}

type ConstructionsScore struct {
	Constructions []ConstructionScore
	BuildingTypes map[uint64]bool
}

func GenerateCommunityConstructionsToScores(conPlanEvents []ConstructionPlanned, conFinEvents []ConstructionFinished, buildingTypes, asteroids map[uint64]bool, mustReach, cap int) []LeaderboardScore {
	byCrews := make(map[uint64]ConstructionsScore)
	for _, cpe := range conPlanEvents {
		if buildingTypes != nil {
			if _, ok := buildingTypes[cpe.BuildingType]; !ok {
				// Pass by building type
				continue
			}
		}
		if asteroids != nil {
			if _, ok := asteroids[cpe.Asteroid.Id]; !ok {
				// Pass by asteroid ID
				continue
			}
		}
	CONSTRUCTION_FINISHED_LOOP:
		for _, cfe := range conFinEvents {
			if cfe.CallerCrew.Id == cpe.CallerCrew.Id && cfe.Building.Id == cpe.Building.Id {
				// Match ConstructionPlanned and ConstructionFinished events
				var constructionsScores ConstructionsScore
				if cs, ok := byCrews[cfe.CallerCrew.Id]; ok {
					constructionsScores = cs
				} else {
					constructionsScores = ConstructionsScore{
						BuildingTypes: make(map[uint64]bool),
					}
				}

				constructionsScores.Constructions = append(constructionsScores.Constructions, ConstructionScore{
					CallerCrew:   cpe.CallerCrew,
					Asteroid:     cpe.Asteroid,
					Building:     cpe.Building,
					BuildingType: cpe.BuildingType,
				})
				constructionsScores.BuildingTypes[cpe.BuildingType] = true
				byCrews[cfe.CallerCrew.Id] = constructionsScores

				break CONSTRUCTION_FINISHED_LOOP
			}
		}
	}

	scores := []LeaderboardScore{}
	for crew, data := range byCrews {
		var buildingTypes []uint64
		for buildingType, include := range data.BuildingTypes {
			if include {
				buildingTypes = append(buildingTypes, buildingType)
			}
		}

		pointsData := map[string]any{
			"complete":      false,
			"buildingTypes": buildingTypes,
			"data":          data,
		}
		if len(data.Constructions) >= 1 {
			pointsData["complete"] = true
		}
		if mustReach != 0 {
			_, isComplete := pointsData["complete"]
			if isComplete && len(data.Constructions) >= mustReach {
				pointsData["mustReachComplete"] = true
			} else {
				pointsData["mustReachComplete"] = false
			}
		}

		if cap != 0 {
			pointsData["cap"] = cap
		}
		scores = append(scores, LeaderboardScore{
			Address:    fmt.Sprintf("%d", crew),
			Score:      uint64(len(data.Constructions)),
			PointsData: pointsData,
		})
	}
	return scores
}

func GenerateC6TheFleet(events []ShipAssemblyFinished) []LeaderboardScore {
	byCrews := make(map[uint64][]uint64)
	for _, e := range events {
		if _, ok := byCrews[e.CallerCrew.Id]; !ok {
			byCrews[e.CallerCrew.Id] = []uint64{}
		}
		byCrews[e.CallerCrew.Id] = append(byCrews[e.CallerCrew.Id], e.Ship.Id)
	}

	scores := []LeaderboardScore{}
	for crew, data := range byCrews {
		isRequirementComplete := false
		isMustReachComplete := false
		if len(data) >= 1 {
			isRequirementComplete = true
		}
		if len(data) >= 200 {
			isMustReachComplete = true
		}
		scores = append(scores, LeaderboardScore{
			Address: fmt.Sprintf("%d", crew),
			Score:   uint64(len(data)),
			PointsData: map[string]any{
				"complete":          isRequirementComplete,
				"mustReachComplete": isMustReachComplete,
				"cap":               1000,
				"data":              data,
			},
		})
	}
	return scores
}

func GenerateC7RockBreaker(events []ResourceExtractionFinished) []LeaderboardScore {
	byCrews := make(map[uint64]uint64)
	for _, e := range events {
		if _, ok := byCrews[e.CallerCrew.Id]; !ok {
			byCrews[e.CallerCrew.Id] = 0
		}
		byCrews[e.CallerCrew.Id] += e.Yield
	}

	scores := []LeaderboardScore{}
	for crew, data := range byCrews {
		isRequirementComplete := false
		isMustReachComplete := false
		if data >= 1000 {
			isRequirementComplete = true
		}
		if data >= 25000000000 {
			isMustReachComplete = true
		}
		scores = append(scores, LeaderboardScore{
			Address: fmt.Sprintf("%d", crew),
			Score:   data,
			PointsData: map[string]any{
				"complete":          isRequirementComplete,
				"mustReachComplete": isMustReachComplete,
				"cap":               50000000000,
			},
		})
	}
	return scores
}

type TransitScore struct {
	TotalAmount          uint64
	MaterialTypesInCargo map[uint64]bool
}

func GenerateC8GoodNewsEveryoneToScores(trFinEvents []TransitFinished, deReEvents []DeliveryReceived) []LeaderboardScore {
	asteroidAPId := uint64(1)
	cTypeMaterials := map[uint64]bool{
		1:  true, // Water
		6:  true, // Carbon Dioxide
		7:  true, // Carbon Monoxide
		8:  true, // Methane
		9:  true, //  Apatite
		10: true, // Bitumen
		11: true, // Calcite
	}

	byCrews := make(map[uint64]TransitScore)
	for _, trfe := range trFinEvents {
		if trfe.Destination.Id != asteroidAPId {
			continue
		}
	DELIVERY_RECEIVED_LOOP:
		for _, dre := range deReEvents {
			if dre.BlockNumber < trfe.BlockNumber {
				// DeliveryReceived should be equal or later then TransitFinished event fired
				continue
			}
			if dre.Dest.Id != asteroidAPId || dre.Dest.Id != trfe.Destination.Id || dre.Origin.Id != trfe.Origin.Id {
				continue
			}
			var transitScores TransitScore
			if ts, ok := byCrews[trfe.CallerCrew.Id]; ok {
				transitScores = ts
			} else {
				transitScores = TransitScore{
					MaterialTypesInCargo: make(map[uint64]bool),
				}
			}
			for _, product := range dre.Products.Snapshot {
				if _, ok := cTypeMaterials[product.Product]; ok {
					// Filter out C-Type materials
					continue
				}
				transitScores.TotalAmount += product.Amount
				transitScores.MaterialTypesInCargo[product.Product] = true
			}
			byCrews[trfe.CallerCrew.Id] = transitScores

			break DELIVERY_RECEIVED_LOOP
		}
	}

	scores := []LeaderboardScore{}
	for crew, data := range byCrews {
		var materialTypes []uint64
		for materialType, include := range data.MaterialTypesInCargo {
			if include {
				materialTypes = append(materialTypes, materialType)
			}
		}

		isRequirementComplete := false
		isMustReachComplete := false
		if data.TotalAmount >= 500000 {
			isRequirementComplete = true
		}
		if data.TotalAmount >= 100000000 {
			isMustReachComplete = true
		}
		scores = append(scores, LeaderboardScore{
			Address: fmt.Sprintf("%d", crew),
			Score:   data.TotalAmount,
			PointsData: map[string]any{
				"complete":          isRequirementComplete,
				"mustReachComplete": isMustReachComplete,
				"cap":               1000000000,
				"materialTypes":     materialTypes,
			},
		})
	}
	return scores
}

func GenerateC9ProspectingPaysOff(events []SamplingDepositFinished) []LeaderboardScore {
	byCrews := make(map[uint64]uint64)
	for _, e := range events {
		if _, ok := byCrews[e.CallerCrew.Id]; !ok {
			byCrews[e.CallerCrew.Id] = 0
		}
		byCrews[e.CallerCrew.Id] += e.InitialYield
	}

	scores := []LeaderboardScore{}
	for crew, data := range byCrews {
		isRequirementComplete := false
		isMustReachComplete := false
		if data >= 1 {
			isRequirementComplete = true
		}
		if data >= 50000 {
			isMustReachComplete = true
		}
		scores = append(scores, LeaderboardScore{
			Address: fmt.Sprintf("%d", crew),
			Score:   data,
			PointsData: map[string]any{
				"cmplete":           isRequirementComplete,
				"mustReachComplete": isMustReachComplete,
				"cap":               1000000000,
			},
		})
	}
	return scores
}

func GenerateC10Potluck(stEventsV1 []MaterialProcessingStartedV1, finEvents []MaterialProcessingFinished) []LeaderboardScore {
	foodFilterId := uint64(129) // Food

	byCrews := make(map[uint64]uint64)
	for _, ste := range stEventsV1 {
		for _, fine := range finEvents {
			if fine.BlockNumber < ste.BlockNumber {
				continue
			}
			if ste.CallerCrew.Id == fine.CallerCrew.Id && ste.Processor.Id == fine.Processor.Id && ste.ProcessorSlot == fine.ProcessorSlot {
				for _, p := range ste.Outputs.Snapshot {
					if p.Product == foodFilterId {
						if _, ok := byCrews[ste.CallerCrew.Id]; !ok {
							byCrews[ste.CallerCrew.Id] = 0
						}
						byCrews[ste.CallerCrew.Id] += p.Amount
					}
				}
			}
		}
	}

	scores := []LeaderboardScore{}
	for crew, data := range byCrews {
		isRequirementComplete := false
		isMustReachComplete := false
		if data >= 5000 {
			isRequirementComplete = true
		}
		if data >= 20000 {
			isMustReachComplete = true
		}
		scores = append(scores, LeaderboardScore{
			Address: fmt.Sprintf("%d", crew),
			Score:   data,
			PointsData: map[string]any{
				"complete":          isRequirementComplete,
				"mustReachComplete": isMustReachComplete,
				"cap":               75000,
			},
		})
	}
	return scores
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
			Score:   uint64(i + 1),
			PointsData: map[string]string{
				"data": crewOwners[k.Str],
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
				ownerCrews[event.From] = FindAndDeleteBigInt(ownerCrews[event.From], event.TokenId)
			}
		} else {
			ownerCrews[event.To] = []*big.Int{event.TokenId}
			if event.From != "0x0" {
				ownerCrews[event.From] = FindAndDeleteBigInt(ownerCrews[event.From], event.TokenId)
			}
		}
	}

	scores := []LeaderboardScore{}
	for owner, crews := range ownerCrews {
		is_complete := false
		if len(crews) >= 5 {
			is_complete = true
		}
		scores = append(scores, LeaderboardScore{
			Address: owner,
			Score:   uint64(len(crews)),
			PointsData: map[string]any{
				"complete": is_complete,
				"data":     crews,
			},
		})
	}

	return scores
}

func Generate1NewRecruitsR1(recEvents []CrewmateRecruited, recV1Events []CrewmateRecruitedV1) []LeaderboardScore {
	byCrews := make(map[uint64]uint64)
	for _, e := range recEvents {
		if _, ok := byCrews[e.CallerCrew.Id]; !ok {
			byCrews[e.CallerCrew.Id] = 0
		}
		byCrews[e.CallerCrew.Id] += 1
	}
	for _, e := range recV1Events {
		if _, ok := byCrews[e.CallerCrew.Id]; !ok {
			byCrews[e.CallerCrew.Id] = 0
		}
		byCrews[e.CallerCrew.Id] += 1
	}

	scores := []LeaderboardScore{}
	for crew, data := range byCrews {
		is_complete := false
		if data >= 5 {
			is_complete = true
		}
		scores = append(scores, LeaderboardScore{
			Address: fmt.Sprintf("%d", crew),
			Score:   data,
			PointsData: map[string]any{
				"complete": is_complete,
			},
		})
	}

	return scores
}

type CrewmateScore struct {
	TotalAmount   uint64
	CrewmateTypes map[uint64]bool
}

func Generate1NewRecruitsR2(recEvents []CrewmateRecruited, recV1Events []CrewmateRecruitedV1) []LeaderboardScore {
	byCrews := make(map[uint64]CrewmateScore)
	for _, e := range recEvents {
		var cremateScore CrewmateScore
		if cs, ok := byCrews[e.CallerCrew.Id]; ok {
			cremateScore = cs
		} else {
			cremateScore = CrewmateScore{
				CrewmateTypes: make(map[uint64]bool),
			}
		}
		cremateScore.TotalAmount += 1
		cremateScore.CrewmateTypes[e.Class] = true
		byCrews[e.CallerCrew.Id] = cremateScore
	}
	for _, e := range recV1Events {
		var cremateScore CrewmateScore
		if cs, ok := byCrews[e.CallerCrew.Id]; ok {
			cremateScore = cs
		} else {
			cremateScore = CrewmateScore{
				CrewmateTypes: make(map[uint64]bool),
			}
		}
		cremateScore.TotalAmount += 1
		cremateScore.CrewmateTypes[e.Class] = true
		byCrews[e.CallerCrew.Id] = cremateScore
	}

	scores := []LeaderboardScore{}
	for crew, data := range byCrews {
		var crewmateTypes []uint64
		for crewmateType, include := range data.CrewmateTypes {
			if include {
				crewmateTypes = append(crewmateTypes, crewmateType)
			}
		}

		is_complete := false
		if len(data.CrewmateTypes) >= 2 {
			is_complete = true
		}
		scores = append(scores, LeaderboardScore{
			Address: fmt.Sprintf("%d", crew),
			Score:   data.TotalAmount,
			PointsData: map[string]any{
				"complete":      is_complete,
				"crewmateTypes": crewmateTypes,
			},
		})
	}
	return scores
}

type OrderScore struct {
	Product uint64
	Amount  uint64
}

type CrewOrdersScore struct {
	CallerCrew Influence_Common_Types_Entity_Entity
	BuyOrders  []OrderScore
	SellOrders []OrderScore
}

func Generate3MarketMakerR1(buyEvents []BuyOrderFilled, sellEvents []SellOrderFilled) []LeaderboardScore {
	byCrews := make(map[uint64]CrewOrdersScore)
	for _, e := range buyEvents {
		crewOrdersScore, ok := byCrews[e.CallerCrew.Id]
		if !ok {
			byCrews[e.CallerCrew.Id] = CrewOrdersScore{}
		}
		crewOrdersScore.BuyOrders = append(crewOrdersScore.BuyOrders, OrderScore{
			Product: e.Product,
			Amount:  e.Amount,
		})
		byCrews[e.CallerCrew.Id] = crewOrdersScore
	}

	for _, e := range sellEvents {
		crewOrdersScore, ok := byCrews[e.CallerCrew.Id]
		if !ok {
			byCrews[e.CallerCrew.Id] = CrewOrdersScore{}
		}
		crewOrdersScore.SellOrders = append(crewOrdersScore.SellOrders, OrderScore{
			Product: e.Product,
			Amount:  e.Amount,
		})
		byCrews[e.CallerCrew.Id] = crewOrdersScore
	}

	scores := []LeaderboardScore{}
	for crew, data := range byCrews {
		is_complete := false
		if len(data.BuyOrders) >= 5 && len(data.SellOrders) >= 1 {
			is_complete = true
		}

		scores = append(scores, LeaderboardScore{
			Address: fmt.Sprintf("%d", crew),
			Score:   uint64(len(data.BuyOrders) + len(data.SellOrders)),
			PointsData: map[string]any{
				"complete": is_complete,
				"data":     data,
			},
		})
	}
	return scores
}

func Generate3MarketMakerR2(buyEvents []BuyOrderCreated, sellEvents []SellOrderCreated) []LeaderboardScore {
	byCrews := make(map[uint64]CrewOrdersScore)
	for _, e := range buyEvents {
		crewOrdersScore, ok := byCrews[e.CallerCrew.Id]
		if !ok {
			byCrews[e.CallerCrew.Id] = CrewOrdersScore{}
		}
		crewOrdersScore.BuyOrders = append(crewOrdersScore.BuyOrders, OrderScore{
			Product: e.Product,
			Amount:  e.Amount,
		})
		byCrews[e.CallerCrew.Id] = crewOrdersScore
	}

	for _, e := range sellEvents {
		crewOrdersScore, ok := byCrews[e.CallerCrew.Id]
		if !ok {
			byCrews[e.CallerCrew.Id] = CrewOrdersScore{}
		}
		crewOrdersScore.SellOrders = append(crewOrdersScore.SellOrders, OrderScore{
			Product: e.Product,
			Amount:  e.Amount,
		})
		byCrews[e.CallerCrew.Id] = crewOrdersScore
	}

	scores := []LeaderboardScore{}
	for crew, data := range byCrews {
		is_complete := false
		if len(data.BuyOrders) >= 5 && len(data.SellOrders) >= 1 {
			is_complete = true
		}

		scores = append(scores, LeaderboardScore{
			Address: fmt.Sprintf("%d", crew),
			Score:   uint64(len(data.BuyOrders) + len(data.SellOrders)),
			PointsData: map[string]any{
				"complete": is_complete,
				"data":     data,
			},
		})
	}
	return scores
}

func Generate4BreakingGroundR1(events []ResourceExtractionFinished) []LeaderboardScore {
	byCrews := make(map[uint64]uint64)
	for _, e := range events {
		if _, ok := byCrews[e.CallerCrew.Id]; !ok {
			byCrews[e.CallerCrew.Id] = 0
		}
		byCrews[e.CallerCrew.Id] += e.Yield
	}

	scores := []LeaderboardScore{}
	for crew, data := range byCrews {
		is_complete := false
		if data >= uint64(10000) {
			is_complete = true
		}
		scores = append(scores, LeaderboardScore{
			Address: fmt.Sprintf("%d", crew),
			Score:   data,
			PointsData: map[string]any{
				"complete": is_complete,
				"data":     data,
			},
		})
	}
	return scores
}

type MineScore struct {
	Resource uint64
	Yield    uint64
}

func Generate4BreakingGroundR2(events []ResourceExtractionFinished) []LeaderboardScore {
	byCrews := make(map[uint64][]MineScore)
	for _, e := range events {
		if _, ok := byCrews[e.CallerCrew.Id]; !ok {
			byCrews[e.CallerCrew.Id] = []MineScore{}
		}
		is_added := false
		for i, d := range byCrews[e.CallerCrew.Id] {
			if d.Resource == e.Resource {
				byCrews[e.CallerCrew.Id][i].Yield += e.Yield
				is_added = true
				break
			}
		}
		if !is_added {
			byCrews[e.CallerCrew.Id] = append(byCrews[e.CallerCrew.Id], MineScore{
				Resource: e.Resource,
				Yield:    e.Yield,
			})
		}
	}

	scores := []LeaderboardScore{}
	for crew, data := range byCrews {
		is_complete := false
		if len(data) >= 4 {
			is_complete = true
		}
		scores = append(scores, LeaderboardScore{
			Address: fmt.Sprintf("%d", crew),
			Score:   uint64(len(data)),
			PointsData: map[string]any{
				"complete": is_complete,
				"data":     data,
			},
		})
	}
	return scores
}

func Generate5CityBuilderR1(conFinEvents []ConstructionFinished, conPlanEvents []ConstructionPlanned) []LeaderboardScore {
	buildingWarehouseType := uint64(1)
	buildingExtractorType := uint64(2)

	byCrews := make(map[uint64][]ConstructionScore)
	for _, cpe := range conPlanEvents {
		if cpe.BuildingType == buildingWarehouseType || cpe.BuildingType == buildingExtractorType {
			continue
		}
		for _, cfe := range conFinEvents {
			if cfe.CallerCrew.Id == cpe.CallerCrew.Id && cfe.Building.Id == cpe.Building.Id {
				if _, ok := byCrews[cfe.CallerCrew.Id]; !ok {
					byCrews[cfe.CallerCrew.Id] = []ConstructionScore{}
				}
				byCrews[cfe.CallerCrew.Id] = append(byCrews[cfe.CallerCrew.Id], ConstructionScore{
					CallerCrew:   cpe.CallerCrew,
					Asteroid:     cpe.Asteroid,
					Building:     cpe.Building,
					BuildingType: cpe.BuildingType,
				})
			}
		}
	}

	scores := []LeaderboardScore{}
	for crew, data := range byCrews {
		scores = append(scores, LeaderboardScore{
			Address: fmt.Sprintf("%d", crew),
			Score:   uint64(len(data)),
			PointsData: map[string]any{
				"complete": true,
				"data":     data,
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

func Generate6ExploreTheStarsR1(events []ShipAssemblyFinished) []LeaderboardScore {
	byCrews := make(map[uint64][]ShipAssemblyFinishedScore, len(events))
	for _, event := range events {
		if _, ok := byCrews[event.CallerCrew.Id]; !ok {
			byCrews[event.CallerCrew.Id] = []ShipAssemblyFinishedScore{}
		}
		byCrews[event.CallerCrew.Id] = append(byCrews[event.CallerCrew.Id], ShipAssemblyFinishedScore{Caller: event.Caller,
			FinishTime:  event.FinishTime,
			Destination: event.Destination,
			Ship:        event.Ship,
		})
	}

	scores := []LeaderboardScore{}
	for crew, data := range byCrews {
		scores = append(scores, LeaderboardScore{
			Address: fmt.Sprintf("%d", crew),
			Score:   uint64(len(data)),
			PointsData: map[string]any{
				"complete": true,
				"data":     data,
			},
		})
	}

	return scores
}

func Generate6ExploreTheStarsR2(events []TransitFinished) []LeaderboardScore {
	asteroidAPId := uint64(1)
	byCrews := make(map[uint64]uint64)
	for _, e := range events {
		if e.Destination.Id == asteroidAPId {
			continue
		}
		if _, ok := byCrews[e.CallerCrew.Id]; !ok {
			byCrews[e.CallerCrew.Id] = 0
		}
		byCrews[e.CallerCrew.Id] += 1
	}

	scores := []LeaderboardScore{}
	for crew, data := range byCrews {
		is_complete := false
		if data >= 1 {
			is_complete = true
		}
		scores = append(scores, LeaderboardScore{
			Address: fmt.Sprintf("%d", crew),
			Score:   data,
			PointsData: map[string]any{
				"complete": is_complete,
			},
		})
	}

	return scores
}

func Generate7ExpandTheColonyR1(conFinEvents []ConstructionFinished, conPlanEvents []ConstructionPlanned) []LeaderboardScore {
	asteroidAPId := uint64(1)

	byCrews := make(map[uint64][]ConstructionScore)
	for _, cpe := range conPlanEvents {
		if cpe.Asteroid.Id == asteroidAPId {
			continue
		}
		for _, cfe := range conFinEvents {
			if cfe.CallerCrew.Id == cpe.CallerCrew.Id && cfe.Building.Id == cpe.Building.Id {
				if _, ok := byCrews[cfe.CallerCrew.Id]; !ok {
					byCrews[cfe.CallerCrew.Id] = []ConstructionScore{}
				}
				byCrews[cfe.CallerCrew.Id] = append(byCrews[cfe.CallerCrew.Id], ConstructionScore{
					CallerCrew:   cpe.CallerCrew,
					Asteroid:     cpe.Asteroid,
					Building:     cpe.Building,
					BuildingType: cpe.BuildingType,
				})
			}
		}
	}

	scores := []LeaderboardScore{}
	for crew, data := range byCrews {
		scores = append(scores, LeaderboardScore{
			Address: fmt.Sprintf("%d", crew),
			Score:   uint64(len(data)),
			PointsData: map[string]any{
				"complete": true,
				"data":     data,
			},
		})
	}
	return scores
}

type DeliveryScore struct {
	Origin          Influence_Common_Types_Entity_Entity
	Destination     Influence_Common_Types_Entity_Entity
	Products        Core_Array_Span_influence_Common_Types_InventoryItem_InventoryItem
	CallerCrew      Influence_Common_Types_Entity_Entity
	cargoHoldAmount uint64
}

func Generate8SpecialDeliveryR1(trEvents []TransitFinished, delEvents []DeliverySent) []LeaderboardScore {
	byCrews := make(map[uint64][]DeliveryScore)
	for _, tre := range trEvents {
		for _, dele := range delEvents {
			if tre.BlockNumber < dele.BlockNumber {
				continue
			}
			if dele.Origin.Id == tre.Origin.Id && dele.Dest.Id == tre.Destination.Id {
				if _, ok := byCrews[tre.CallerCrew.Id]; !ok {
					byCrews[tre.CallerCrew.Id] = []DeliveryScore{}
				}
				delivery := DeliveryScore{
					Origin:      tre.Origin,
					Destination: tre.Destination,
					CallerCrew:  tre.CallerCrew,
					Products:    dele.Products,
				}
				for _, p := range dele.Products.Snapshot {
					delivery.cargoHoldAmount += p.Amount
				}
				byCrews[tre.CallerCrew.Id] = append(byCrews[tre.CallerCrew.Id], delivery)
			}
		}
	}

	scores := []LeaderboardScore{}
	for crew, data := range byCrews {
		var totalCargoHoldAmount uint64
		for _, d := range data {
			totalCargoHoldAmount += d.cargoHoldAmount
		}
		is_complete := false
		if totalCargoHoldAmount >= 1000000 {
			is_complete = true
		}
		scores = append(scores, LeaderboardScore{
			Address: fmt.Sprintf("%d", crew),
			Score:   totalCargoHoldAmount,
			PointsData: map[string]any{
				"complete": is_complete,
				"data":     data,
			},
		})
	}

	return scores
}

func Generate9DinnerIsServedR1(events []FoodSupplied, eventsV1 []FoodSuppliedV1) []LeaderboardScore {
	byCrews := make(map[uint64]uint64)
	for _, e := range events {
		if _, ok := byCrews[e.CallerCrew.Id]; !ok {
			byCrews[e.CallerCrew.Id] = 0
		}
		byCrews[e.CallerCrew.Id] += e.Food
	}

	for _, e := range eventsV1 {
		if _, ok := byCrews[e.CallerCrew.Id]; !ok {
			byCrews[e.CallerCrew.Id] = 0
		}
		byCrews[e.CallerCrew.Id] += e.Food
	}

	scores := []LeaderboardScore{}
	for crew, data := range byCrews {
		is_complete := false
		if data >= 10000 {
			is_complete = true
		}
		scores = append(scores, LeaderboardScore{
			Address: fmt.Sprintf("%d", crew),
			Score:   data,
			PointsData: map[string]any{
				"complete": is_complete,
			},
		})
	}
	return scores
}
