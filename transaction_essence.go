package iotago

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/iotaledger/hive.go/serializer"

	"golang.org/x/crypto/blake2b"
)

// TransactionEssenceType defines the type of transaction.
type TransactionEssenceType = byte

const (
	// TransactionEssenceNormal denotes a standard transaction essence.
	TransactionEssenceNormal TransactionEssenceType = iota

	// TransactionEssenceMinByteSize defines the minimum size of a TransactionEssence.
	TransactionEssenceMinByteSize = serializer.TypeDenotationByteSize + serializer.UInt16ByteSize + serializer.UInt16ByteSize + serializer.PayloadLengthByteSize

	// MaxInputsCount defines the maximum amount of inputs within a TransactionEssence.
	MaxInputsCount = 127
	// MinInputsCount defines the minimum amount of inputs within a TransactionEssence.
	MinInputsCount = 1
	// MaxOutputsCount defines the maximum amount of outputs within a TransactionEssence.
	MaxOutputsCount = 127
	// MinOutputsCount defines the minimum amount of inputs within a TransactionEssence.
	MinOutputsCount = 1
)

var (
	// ErrMinInputsNotReached gets returned if the count of inputs is too small.
	ErrMinInputsNotReached = fmt.Errorf("min %d input(s) are required within a transaction", MinInputsCount)
	// ErrMinOutputsNotReached gets returned if the count of outputs is too small.
	ErrMinOutputsNotReached = fmt.Errorf("min %d output(s) are required within a transaction", MinOutputsCount)
	// ErrInputUTXORefsNotUnique gets returned if multiple inputs reference the same UTXO.
	ErrInputUTXORefsNotUnique = errors.New("inputs must each reference a unique UTXO")
	// ErrOutputAddrNotUnique gets returned if multiple outputs deposit to the same address.
	ErrOutputAddrNotUnique = errors.New("outputs must each deposit to a unique address")
	// ErrOutputRequiresSenderFeatureBlock gets returned if an output does not contain a SenderFeatureBlock even though another FeatureBlock requires it.
	ErrOutputRequiresSenderFeatureBlock = errors.New("output does not contain SenderFeatureBlock")
	// ErrAliasOutputNonEmptyState gets returned if an AliasOutput with zeroed AliasID contains state (counters non-zero etc.).
	ErrAliasOutputNonEmptyState = errors.New("alias output is not empty state")
	// ErrAliasOutputCyclicAddress gets returned if an AliasOutput's AliasID results into the same address as the State/Governance controller.
	ErrAliasOutputCyclicAddress = errors.New("alias output's AliasID corresponds to state and/or governance controller")
	// ErrNFTOutputCyclicAddress gets returned if an NFTOutput's NFTID results into the same address as the address field within the output.
	ErrNFTOutputCyclicAddress = errors.New("nft output's NFTID corresponds to address field")
	// ErrFoundryOutputInvalidMaximumSupply gets returned when a FoundryOutput's MaximumSupply is invalid.
	ErrFoundryOutputInvalidMaximumSupply = errors.New("foundry output's maximum supply is invalid")
	// ErrFoundryOutputInvalidCirculatingSupply gets returned when a FoundryOutput's CirculatingSupply is invalid.
	ErrFoundryOutputInvalidCirculatingSupply = errors.New("foundry output's circulating supply is invalid")
	// ErrOutputsSumExceedsTotalSupply gets returned if the sum of the output deposits exceeds the total supply of tokens.
	ErrOutputsSumExceedsTotalSupply = errors.New("accumulated output balance exceeds total supply")
	// ErrOutputDepositsMoreThanTotalSupply gets returned if an output deposits more than the total supply.
	ErrOutputDepositsMoreThanTotalSupply = errors.New("an output can not deposit more than the total supply")
	// ErrOutputsExceedMaxNativeTokensCount gets returned if outputs exceed the MaxNativeTokensCount.
	ErrOutputsExceedMaxNativeTokensCount = errors.New("outputs exceeds max native tokens count")

	// restrictions around input within a transaction.
	inputsArrayBound = serializer.ArrayRules{
		Min:            MinInputsCount,
		Max:            MaxInputsCount,
		ValidationMode: serializer.ArrayValidationModeNoDuplicates,
	}

	// restrictions around outputs within a transaction.
	outputsArrayBound = serializer.ArrayRules{
		Min:            MinOutputsCount,
		Max:            MaxOutputsCount,
		ValidationMode: serializer.ArrayValidationModeNone,
	}
)

// TransactionEssenceSelector implements SerializableSelectorFunc for transaction essence types.
func TransactionEssenceSelector(txType uint32) (serializer.Serializable, error) {
	var seri serializer.Serializable
	switch byte(txType) {
	case TransactionEssenceNormal:
		seri = &TransactionEssence{}
	default:
		return nil, fmt.Errorf("%w: type byte %d", ErrUnknownTransactionEssenceType, txType)
	}
	return seri, nil
}

// TransactionEssence is the essence part of a Transaction.
type TransactionEssence struct {
	// The inputs of this transaction.
	Inputs serializer.Serializables `json:"inputs"`
	// The outputs of this transaction.
	Outputs serializer.Serializables `json:"outputs"`
	// The optional embedded payload.
	Payload serializer.Serializable `json:"payload"`
}

// SigningMessage returns the to be signed message.
func (u *TransactionEssence) SigningMessage() ([]byte, error) {
	essenceBytes, err := u.Serialize(serializer.DeSeriModePerformValidation | serializer.DeSeriModePerformLexicalOrdering)
	if err != nil {
		return nil, err
	}
	essenceBytesHash := blake2b.Sum256(essenceBytes)
	return essenceBytesHash[:], nil
}

func (u *TransactionEssence) Deserialize(data []byte, deSeriMode serializer.DeSerializationMode) (int, error) {
	return serializer.NewDeserializer(data).
		AbortIf(func(err error) error {
			if deSeriMode.HasMode(serializer.DeSeriModePerformValidation) {
				if err := serializer.CheckMinByteLength(TransactionEssenceMinByteSize, len(data)); err != nil {
					return fmt.Errorf("invalid transaction essence bytes: %w", err)
				}
				if err := serializer.CheckTypeByte(data, TransactionEssenceNormal); err != nil {
					return fmt.Errorf("unable to deserialize transaction essence: %w", err)
				}
			}
			return nil
		}).
		Skip(serializer.SmallTypeDenotationByteSize, func(err error) error {
			return fmt.Errorf("unable to skip transaction essence ID during deserialization: %w", err)
		}).
		ReadSliceOfObjects(func(seri serializer.Serializables) { u.Inputs = seri }, deSeriMode, serializer.SeriLengthPrefixTypeAsUint16, serializer.TypeDenotationByte, func(ty uint32) (serializer.Serializable, error) {
			switch ty {
			case uint32(InputUTXO):
			default:
				return nil, fmt.Errorf("transaction essence can only contain UTXO input as inputs but got type ID %d: %w", ty, ErrUnsupportedObjectType)
			}
			return InputSelector(ty)
		}, &inputsArrayBound, func(err error) error {
			return fmt.Errorf("unable to deserialize inputs of transaction essence: %w", err)
		}).
		ReadSliceOfObjects(func(seri serializer.Serializables) { u.Outputs = seri }, deSeriMode, serializer.SeriLengthPrefixTypeAsUint16, serializer.TypeDenotationByte, func(ty uint32) (serializer.Serializable, error) {
			if !outputTypeSupportedByTxEssence(ty) {
				return nil, fmt.Errorf("transaction essence can only contain simple/extended/alias/foundry/nft outputs types but got type ID %d: %w", ty, ErrUnsupportedObjectType)
			}
			return OutputSelector(ty)
		}, &outputsArrayBound, func(err error) error {
			return fmt.Errorf("unable to deserialize outputs of transaction essence: %w", err)
		}).
		AbortIf(func(err error) error {
			if deSeriMode.HasMode(serializer.DeSeriModePerformValidation) {
				if err := ValidateOutputs(u.Outputs, OutputsPredicateAddrUnique()); err != nil {
					return fmt.Errorf("%w: unable to deserialize outputs of transaction essence since they are invalid", err)
				}
			}
			return nil
		}).
		ReadPayload(func(seri serializer.Serializable) { u.Payload = seri }, deSeriMode,
			func(ty uint32) (serializer.Serializable, error) {
				if ty != IndexationPayloadTypeID {
					return nil, fmt.Errorf("transaction essence can only contain an indexation payload: %w", ErrUnsupportedPayloadType)
				}
				return PayloadSelector(ty)
			},
			func(err error) error {
				return fmt.Errorf("unable to deserialize outputs of transaction essence: %w", err)
			}).
		AbortIf(func(err error) error {
			if deSeriMode.HasMode(serializer.DeSeriModePerformValidation) {
				if u.Payload != nil {
					// supports only indexation payloads
					if _, isIndexationPayload := u.Payload.(*Indexation); !isIndexationPayload {
						return fmt.Errorf("%w: transaction essences only allow embedded indexation payloads but got %T instead", serializer.ErrInvalidBytes, u.Payload)
					}
				}
			}
			return nil
		}).
		Done()
}

func outputTypeSupportedByTxEssence(ty uint32) bool {
	switch ty {
	case uint32(OutputSimple):
	case uint32(OutputExtended):
	case uint32(OutputAlias):
	case uint32(OutputFoundry):
	case uint32(OutputNFT):
	default:
		return false
	}
	return true
}

func (u *TransactionEssence) Serialize(deSeriMode serializer.DeSerializationMode) (data []byte, err error) {
	var inputsWrittenConsumer serializer.WrittenObjectConsumer
	if deSeriMode.HasMode(serializer.DeSeriModePerformValidation) {

		if u.Payload != nil {
			if _, isIndexationPayload := u.Payload.(*Indexation); !isIndexationPayload {
				return nil, fmt.Errorf("%w: transaction essences only allow embedded indexation payloads but got %T instead", serializer.ErrInvalidBytes, u.Payload)
			}
		}
		if inputsArrayBound.ValidationMode.HasMode(serializer.ArrayValidationModeNoDuplicates) {
			inputsUniqueValidator := inputsArrayBound.ElementUniqueValidator()
			inputsWrittenConsumer = func(index int, written []byte) error {
				if err := inputsUniqueValidator(index, written); err != nil {
					return fmt.Errorf("%w: unable to serialize inputs of transaction essence since inputs are not unique", err)
				}
				return nil
			}
		}
	}

	return serializer.NewSerializer().
		AbortIf(func(err error) error {
			if deSeriMode.HasMode(serializer.DeSeriModePerformValidation) {
				if err := u.SyntacticallyValidate(); err != nil {
					return err
				}
			}
			return nil
		}).
		WriteNum(TransactionEssenceNormal, func(err error) error {
			return fmt.Errorf("unable to serialize transaction essence type ID: %w", err)
		}).
		WriteSliceOfObjects(u.Inputs, deSeriMode, serializer.SeriLengthPrefixTypeAsUint16, inputsWrittenConsumer, func(err error) error {
			return fmt.Errorf("unable to serialize transaction essence inputs: %w", err)
		}).
		WriteSliceOfObjects(u.Outputs, deSeriMode, serializer.SeriLengthPrefixTypeAsUint16, nil, func(err error) error {
			return fmt.Errorf("unable to serialize transaction essence outputs: %w", err)
		}).
		WritePayload(u.Payload, deSeriMode, func(err error) error {
			return fmt.Errorf("unable to serialize transaction essence's embedded output: %w", err)
		}).
		Serialize()
}

func (u *TransactionEssence) MarshalJSON() ([]byte, error) {
	jTransactionEssence := &jsonTransactionEssence{
		Inputs:  make([]*json.RawMessage, len(u.Inputs)),
		Outputs: make([]*json.RawMessage, len(u.Outputs)),
		Payload: nil,
	}
	jTransactionEssence.Type = int(TransactionEssenceNormal)

	for i, input := range u.Inputs {
		inputJson, err := input.MarshalJSON()
		if err != nil {
			return nil, err
		}
		rawMsgInputJson := json.RawMessage(inputJson)
		jTransactionEssence.Inputs[i] = &rawMsgInputJson

	}
	for i, output := range u.Outputs {
		outputJson, err := output.MarshalJSON()
		if err != nil {
			return nil, err
		}
		rawMsgOutputJson := json.RawMessage(outputJson)
		jTransactionEssence.Outputs[i] = &rawMsgOutputJson
	}

	if u.Payload != nil {
		jsonPayload, err := u.Payload.MarshalJSON()
		if err != nil {
			return nil, err
		}
		rawMsgJsonPayload := json.RawMessage(jsonPayload)
		jTransactionEssence.Payload = &rawMsgJsonPayload
	}
	return json.Marshal(jTransactionEssence)
}

func (u *TransactionEssence) UnmarshalJSON(bytes []byte) error {
	jTransactionEssence := &jsonTransactionEssence{}
	if err := json.Unmarshal(bytes, jTransactionEssence); err != nil {
		return err
	}
	seri, err := jTransactionEssence.ToSerializable()
	if err != nil {
		return err
	}
	*u = *seri.(*TransactionEssence)
	return nil
}

// SyntacticallyValidate checks whether the transaction essence is syntactically valid by checking whether:
//	- every input references a unique UTXO and has valid UTXO index bounds
//	- every output (per type) deposits more than zero
//	- the accumulated deposit output is not over the total supply
// The function does not syntactically validate the input or outputs themselves.
func (u *TransactionEssence) SyntacticallyValidate() error {

	if len(u.Inputs) == 0 {
		return ErrMinInputsNotReached
	}

	if len(u.Outputs) == 0 {
		return ErrMinOutputsNotReached
	}

	if err := ValidateInputs(u.Inputs,
		InputsPredicateUnique(),
		InputsPredicateIndicesWithinBounds(),
	); err != nil {
		return err
	}

	if err := ValidateOutputs(u.Outputs,
		OutputsPredicateDepositAmount(),
		OutputsPredicateNativeTokensCount(),
		OutputsPredicateSenderFeatureBlockRequirement(),
		OutputsPredicateFoundry(),
	); err != nil {
		return err
	}

	return nil
}

// jsonTransactionEssenceSelector selects the json transaction essence object for the given type.
func jsonTransactionEssenceSelector(ty int) (JSONSerializable, error) {
	var obj JSONSerializable
	switch byte(ty) {
	case TransactionEssenceNormal:
		obj = &jsonTransactionEssence{}
	default:
		return nil, fmt.Errorf("unable to decode transaction essence type from JSON: %w", ErrUnknownTransactionEssenceType)
	}

	return obj, nil
}

// jsonTransactionEssence defines the json representation of a TransactionEssence.
type jsonTransactionEssence struct {
	Type    int                `json:"type"`
	Inputs  []*json.RawMessage `json:"inputs"`
	Outputs []*json.RawMessage `json:"outputs"`
	Payload *json.RawMessage   `json:"payload"`
}

func (j *jsonTransactionEssence) ToSerializable() (serializer.Serializable, error) {
	unsigTx := &TransactionEssence{
		Inputs:  make(serializer.Serializables, len(j.Inputs)),
		Outputs: make(serializer.Serializables, len(j.Outputs)),
		Payload: nil,
	}

	for i, input := range j.Inputs {
		jsonInput, err := DeserializeObjectFromJSON(input, jsonInputSelector)
		if err != nil {
			return nil, fmt.Errorf("unable to decode input type from JSON, pos %d: %w", i, err)
		}
		input, err := jsonInput.ToSerializable()
		if err != nil {
			return nil, fmt.Errorf("pos %d: %w", i, err)
		}
		unsigTx.Inputs[i] = input
	}

	for i, output := range j.Outputs {
		jsonOutput, err := DeserializeObjectFromJSON(output, jsonOutputSelector)
		if err != nil {
			return nil, fmt.Errorf("unable to decode output type from JSON, pos %d: %w", i, err)
		}
		output, err := jsonOutput.ToSerializable()
		if err != nil {
			return nil, fmt.Errorf("pos %d: %w", i, err)
		}
		unsigTx.Outputs[i] = output
	}

	if j.Payload == nil {
		return unsigTx, nil
	}

	jsonPayload, err := DeserializeObjectFromJSON(j.Payload, jsonPayloadSelector)
	if err != nil {
		return nil, err
	}

	if _, isJSONIndexationPayload := jsonPayload.(*jsonIndexation); !isJSONIndexationPayload {
		return nil, fmt.Errorf("%w: transaction essences only allow embedded indexation payloads but got type %T instead", ErrInvalidJSON, jsonPayload)
	}

	unsigTx.Payload, err = jsonPayload.ToSerializable()
	if err != nil {
		return nil, fmt.Errorf("unable to decode inner transaction essence payload: %w", err)
	}

	return unsigTx, nil
}
