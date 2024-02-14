// This file was generated by seer: https://github.com/moonstream-to/seer.
// seer version: 0.1.0
// seer command: seer starknet generate --package influence
// Warning: Edit at your own risk. Any edits you make will NOT survive the next code generation.

package crew

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"time"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/consensys/gnark-crypto/ecc/stark-curve/fp"
)

var ErrIncorrectParameters error = errors.New("incorrect parameters")

func ParseUint64(parameters []*felt.Felt) (uint64, int, error) {
	if len(parameters) < 1 {
		return 0, 0, ErrIncorrectParameters
	}
	return parameters[0].Uint64(), 1, nil
}

func ParseBigInt(parameters []*felt.Felt) (*big.Int, int, error) {
	if len(parameters) < 1 {
		return nil, 0, ErrIncorrectParameters
	}
	result := big.NewInt(0)
	result = parameters[0].BigInt(result)
	return result, 1, nil
}

func ParseString(parameters []*felt.Felt) (string, int, error) {
	if len(parameters) < 1 {
		return "", 0, ErrIncorrectParameters
	}
	return parameters[0].String(), 1, nil
}

func ParseArray[T any](parser func(parameters []*felt.Felt) (T, int, error)) func(parameters []*felt.Felt) ([]T, int, error) {
	return func(parameters []*felt.Felt) ([]T, int, error) {
		if len(parameters) < 1 {
			return nil, 0, ErrIncorrectParameters
		}

		arrayLengthRaw := parameters[0].Uint64()
		arrayLength := int(arrayLengthRaw)
		if len(parameters) < arrayLength+1 {
			return nil, 0, ErrIncorrectParameters
		}

		result := make([]T, arrayLength)
		currentIndex := 1
		for i := 0; i < arrayLength; i++ {
			parsed, consumed, err := parser(parameters[currentIndex:])
			if err != nil {
				return nil, 0, err
			}
			result[i] = parsed
			currentIndex += consumed
		}

		return result, currentIndex, nil
	}
}

var ErrIncorrectEventKey error = errors.New("incorrect event key")

type RawEvent struct {
	BlockNumber     uint64
	BlockHash       *felt.Felt
	TransactionHash *felt.Felt
	FromAddress     *felt.Felt
	PrimaryKey      *felt.Felt
	Keys            []*felt.Felt
	Parameters      []*felt.Felt
}

func FeltFromHexString(hexString string) (*felt.Felt, error) {
	fieldAdditiveIdentity := fp.NewElement(0)

	if hexString[:2] == "0x" {
		hexString = hexString[2:]
	}
	decodedString, decodeErr := hex.DecodeString(hexString)
	if decodeErr != nil {
		return nil, decodeErr
	}
	derivedFelt := felt.NewFelt(&fieldAdditiveIdentity)
	derivedFelt.SetBytes(decodedString)

	return derivedFelt, nil
}

func AllEventsFilter(fromBlock, toBlock uint64, contractAddress string) (*rpc.EventFilter, error) {
	result := rpc.EventFilter{FromBlock: rpc.BlockID{Number: &fromBlock}, ToBlock: rpc.BlockID{Number: &toBlock}}

	fieldAdditiveIdentity := fp.NewElement(0)

	if contractAddress != "" {
		if contractAddress[:2] == "0x" {
			contractAddress = contractAddress[2:]
		}
		decodedAddress, decodeErr := hex.DecodeString(contractAddress)
		if decodeErr != nil {
			return &result, decodeErr
		}
		result.Address = felt.NewFelt(&fieldAdditiveIdentity)
		result.Address.SetBytes(decodedAddress)
	}

	result.Keys = [][]*felt.Felt{{}}

	return &result, nil
}

func ContractEvents(ctx context.Context, provider *rpc.Provider, contractAddress string, outChan chan<- RawEvent, hotThreshold int, hotInterval, coldInterval time.Duration, fromBlock, toBlock uint64, confirmations, batchSize int) error {
	defer func() { close(outChan) }()

	type CrawlCursor struct {
		FromBlock         uint64
		ToBlock           uint64
		ContinuationToken string
		Interval          time.Duration
		Heat              int
	}

	cursor := CrawlCursor{FromBlock: fromBlock, ToBlock: toBlock, ContinuationToken: "", Interval: hotInterval, Heat: 0}

	count := 0

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(cursor.Interval):
			count++
			if cursor.ToBlock == 0 {
				currentblock, blockErr := provider.BlockNumber(ctx)
				if blockErr != nil {
					return blockErr
				}
				cursor.ToBlock = currentblock - uint64(confirmations)
			}

			if cursor.ToBlock <= cursor.FromBlock {
				// Crawl is cold, slow things down.
				cursor.Interval = coldInterval

				if toBlock == 0 {
					// If the crawl is continuous, breaks out of select, not for loop.
					// This effects a wait for the given interval.
					break
				} else {
					// If crawl is not continuous, just ends the crawl.
					return nil
				}
			}

			filter, filterErr := AllEventsFilter(cursor.FromBlock, cursor.ToBlock, contractAddress)
			if filterErr != nil {
				return filterErr
			}

			eventsInput := rpc.EventsInput{
				EventFilter:       *filter,
				ResultPageRequest: rpc.ResultPageRequest{ChunkSize: batchSize, ContinuationToken: cursor.ContinuationToken},
			}

			eventsChunk, getEventsErr := provider.Events(ctx, eventsInput)
			if getEventsErr != nil {
				return getEventsErr
			}

			for _, event := range eventsChunk.Events {
				crawledEvent := RawEvent{
					BlockNumber:     event.BlockNumber,
					BlockHash:       event.BlockHash,
					TransactionHash: event.TransactionHash,
					FromAddress:     event.FromAddress,
					PrimaryKey:      event.Keys[0],
					Keys:            event.Keys,
					Parameters:      event.Data,
				}

				outChan <- crawledEvent
			}

			if eventsChunk.ContinuationToken != "" {
				cursor.ContinuationToken = eventsChunk.ContinuationToken
				cursor.Interval = hotInterval
			} else {
				cursor.FromBlock = cursor.ToBlock + 1
				cursor.ToBlock = toBlock
				cursor.ContinuationToken = ""
				if len(eventsChunk.Events) > 0 {
					cursor.Heat++
					if cursor.Heat >= hotThreshold {
						cursor.Interval = hotInterval
					}
				} else {
					cursor.Heat = 0
					cursor.Interval = coldInterval
				}
			}
		}
	}
}

// ABI: influence::contracts::crew::Crew::Transfer

// ABI name for event
var Event_Influence_Contracts_Crew_Crew_Transfer string = "influence::contracts::crew::Crew::Transfer"

// Starknet hash for the event, as it appears in Starknet event logs.
var Hash_Influence_Contracts_Crew_Crew_Transfer string = "99cd8bde557814842a3121e8ddfd433a539b8c9f14bf31ebf108d12e6196e9"

// Influence_Contracts_Crew_Crew_Transfer is the Go struct corresponding to the influence::contracts::crew::Crew::Transfer event.
type Influence_Contracts_Crew_Crew_Transfer struct {
	From    string
	To      string
	TokenId *big.Int
}

// ParseInfluence_Contracts_Crew_Crew_Transfer parses a Influence_Contracts_Crew_Crew_Transfer event from a list of felts. This function returns a tuple of:
// 1. The parsed Influence_Contracts_Crew_Crew_Transfer struct representing the event
// 2. The number of field elements consumed in the parse
// 3. An error if the parse failed, nil otherwise
func ParseInfluence_Contracts_Crew_Crew_Transfer(parameters []*felt.Felt) (Influence_Contracts_Crew_Crew_Transfer, int, error) {
	currentIndex := 0
	result := Influence_Contracts_Crew_Crew_Transfer{}

	value0, consumed, err := ParseString(parameters[currentIndex:])
	if err != nil {
		return result, 0, err
	}
	result.From = value0
	currentIndex += consumed

	value1, consumed, err := ParseString(parameters[currentIndex:])
	if err != nil {
		return result, 0, err
	}
	result.To = value1
	currentIndex += consumed

	value2, consumed, err := ParseBigInt(parameters[currentIndex:])
	if err != nil {
		return result, 0, err
	}
	result.TokenId = value2
	currentIndex += consumed

	return result, currentIndex + 1, nil
}

var EVENT_UNKNOWN = "UNKNOWN"

type ParsedEvent struct {
	Name  string
	Event interface{}
}

type PartialEvent struct {
	Name  string
	Event json.RawMessage
}

type EventParser struct {
	Event_Influence_Contracts_Crew_Crew_Approval_Felt       *felt.Felt
	Event_Influence_Contracts_Crew_Crew_ApprovalForAll_Felt *felt.Felt
	Event_Influence_Contracts_Crew_Crew_Transfer_Felt       *felt.Felt
}

func NewEventParser() (*EventParser, error) {
	var feltErr error
	parser := &EventParser{}

	parser.Event_Influence_Contracts_Crew_Crew_Approval_Felt, feltErr = FeltFromHexString(Hash_Influence_Contracts_Crew_Crew_Approval)
	if feltErr != nil {
		return parser, feltErr
	}

	parser.Event_Influence_Contracts_Crew_Crew_ApprovalForAll_Felt, feltErr = FeltFromHexString(Hash_Influence_Contracts_Crew_Crew_ApprovalForAll)
	if feltErr != nil {
		return parser, feltErr
	}

	parser.Event_Influence_Contracts_Crew_Crew_Transfer_Felt, feltErr = FeltFromHexString(Hash_Influence_Contracts_Crew_Crew_Transfer)
	if feltErr != nil {
		return parser, feltErr
	}

	return parser, nil
}

func (p *EventParser) Parse(event RawEvent) (ParsedEvent, error) {
	defaultResult := ParsedEvent{Name: EVENT_UNKNOWN, Event: event}

	if p.Event_Influence_Contracts_Crew_Crew_Approval_Felt.Cmp(event.PrimaryKey) == 0 {
		parsedEvent, _, parseErr := ParseInfluence_Contracts_Crew_Crew_Approval(event.Parameters)
		if parseErr != nil {
			return defaultResult, parseErr
		}
		return ParsedEvent{Name: Event_Influence_Contracts_Crew_Crew_Approval, Event: parsedEvent}, nil
	}
	if p.Event_Influence_Contracts_Crew_Crew_ApprovalForAll_Felt.Cmp(event.PrimaryKey) == 0 {
		parsedEvent, _, parseErr := ParseInfluence_Contracts_Crew_Crew_ApprovalForAll(event.Parameters)
		if parseErr != nil {
			return defaultResult, parseErr
		}
		return ParsedEvent{Name: Event_Influence_Contracts_Crew_Crew_ApprovalForAll, Event: parsedEvent}, nil
	}
	if p.Event_Influence_Contracts_Crew_Crew_Transfer_Felt.Cmp(event.PrimaryKey) == 0 {
		parsedEvent, _, parseErr := ParseInfluence_Contracts_Crew_Crew_Transfer(event.Parameters)
		if parseErr != nil {
			return defaultResult, parseErr
		}
		return ParsedEvent{Name: Event_Influence_Contracts_Crew_Crew_Transfer, Event: parsedEvent}, nil
	}
	return defaultResult, nil
}

// ABI: core::bool

// Core_Bool is an alias for uint64
type Core_Bool = uint64

// ParseCore_Bool parses a Core_Bool from a list of felts. This function returns a tuple of:
// 1. The parsed Core_Bool
// 2. The number of field elements consumed in the parse
// 3. An error if the parse failed, nil otherwise
func ParseCore_Bool(parameters []*felt.Felt) (Core_Bool, int, error) {
	if len(parameters) < 1 {
		return 0, 0, ErrIncorrectParameters
	}
	return Core_Bool(parameters[0].Uint64()), 1, nil
}

// This function returns the string representation of a Core_Bool enum. This is the enum value from the ABI definition of the enum.
func EvaluateCore_Bool(raw Core_Bool) string {
	switch raw {
	case 0:
		return "False"
	case 1:
		return "True"
	}
	return "UNKNOWN"
}

// ABI: core::array::Span::<core::felt252>

// Core_Array_Span_core_Felt252 is the Go struct corresponding to the core::array::Span::<core::felt252> struct.
type Core_Array_Span_core_Felt252 struct {
	Snapshot []string
}

// ParseCore_Array_Span_core_Felt252 parses a Core_Array_Span_core_Felt252 struct from a list of felts. This function returns a tuple of:
// 1. The parsed Core_Array_Span_core_Felt252 struct
// 2. The number of field elements consumed in the parse
// 3. An error if the parse failed, nil otherwise
func ParseCore_Array_Span_core_Felt252(parameters []*felt.Felt) (Core_Array_Span_core_Felt252, int, error) {
	currentIndex := 0
	result := Core_Array_Span_core_Felt252{}

	value0, consumed, err := ParseArray[string](ParseString)(parameters[currentIndex:])
	if err != nil {
		return result, 0, err
	}
	result.Snapshot = value0
	currentIndex += consumed

	return result, currentIndex, nil
}

// ABI: influence::contracts::crew::Crew::Approval

// ABI name for event
var Event_Influence_Contracts_Crew_Crew_Approval string = "influence::contracts::crew::Crew::Approval"

// Starknet hash for the event, as it appears in Starknet event logs.
var Hash_Influence_Contracts_Crew_Crew_Approval string = "0134692b230b9e1ffa39098904722134159652b09c5bc41d88d6698779d228ff"

// Influence_Contracts_Crew_Crew_Approval is the Go struct corresponding to the influence::contracts::crew::Crew::Approval event.
type Influence_Contracts_Crew_Crew_Approval struct {
	Owner    string
	Approved string
	TokenId  *big.Int
}

// ParseInfluence_Contracts_Crew_Crew_Approval parses a Influence_Contracts_Crew_Crew_Approval event from a list of felts. This function returns a tuple of:
// 1. The parsed Influence_Contracts_Crew_Crew_Approval struct representing the event
// 2. The number of field elements consumed in the parse
// 3. An error if the parse failed, nil otherwise
func ParseInfluence_Contracts_Crew_Crew_Approval(parameters []*felt.Felt) (Influence_Contracts_Crew_Crew_Approval, int, error) {
	currentIndex := 0
	result := Influence_Contracts_Crew_Crew_Approval{}

	value0, consumed, err := ParseString(parameters[currentIndex:])
	if err != nil {
		return result, 0, err
	}
	result.Owner = value0
	currentIndex += consumed

	value1, consumed, err := ParseString(parameters[currentIndex:])
	if err != nil {
		return result, 0, err
	}
	result.Approved = value1
	currentIndex += consumed

	value2, consumed, err := ParseBigInt(parameters[currentIndex:])
	if err != nil {
		return result, 0, err
	}
	result.TokenId = value2
	currentIndex += consumed

	return result, currentIndex + 1, nil
}

// ABI: influence::contracts::crew::Crew::ApprovalForAll

// ABI name for event
var Event_Influence_Contracts_Crew_Crew_ApprovalForAll string = "influence::contracts::crew::Crew::ApprovalForAll"

// Starknet hash for the event, as it appears in Starknet event logs.
var Hash_Influence_Contracts_Crew_Crew_ApprovalForAll string = "06ad9ed7b6318f1bcffefe19df9aeb40d22c36bed567e1925a5ccde0536edd"

// Influence_Contracts_Crew_Crew_ApprovalForAll is the Go struct corresponding to the influence::contracts::crew::Crew::ApprovalForAll event.
type Influence_Contracts_Crew_Crew_ApprovalForAll struct {
	Owner    string
	Operator string
	Approved Core_Bool
}

// ParseInfluence_Contracts_Crew_Crew_ApprovalForAll parses a Influence_Contracts_Crew_Crew_ApprovalForAll event from a list of felts. This function returns a tuple of:
// 1. The parsed Influence_Contracts_Crew_Crew_ApprovalForAll struct representing the event
// 2. The number of field elements consumed in the parse
// 3. An error if the parse failed, nil otherwise
func ParseInfluence_Contracts_Crew_Crew_ApprovalForAll(parameters []*felt.Felt) (Influence_Contracts_Crew_Crew_ApprovalForAll, int, error) {
	currentIndex := 0
	result := Influence_Contracts_Crew_Crew_ApprovalForAll{}

	value0, consumed, err := ParseString(parameters[currentIndex:])
	if err != nil {
		return result, 0, err
	}
	result.Owner = value0
	currentIndex += consumed

	value1, consumed, err := ParseString(parameters[currentIndex:])
	if err != nil {
		return result, 0, err
	}
	result.Operator = value1
	currentIndex += consumed

	value2, consumed, err := ParseCore_Bool(parameters[currentIndex:])
	if err != nil {
		return result, 0, err
	}
	result.Approved = value2
	currentIndex += consumed

	return result, currentIndex + 1, nil
}
