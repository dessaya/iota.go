package iotago

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/iotaledger/hive.go/serializer"

	"golang.org/x/crypto/blake2b"
)

const (
	// TransactionIDLength defines the length of a Transaction ID.
	TransactionIDLength = blake2b.Size256
)

var (
	// ErrMissingUTXO gets returned if an UTXO is missing to commence a certain operation.
	ErrMissingUTXO = errors.New("missing utxo")
	// ErrInputOutputSumMismatch gets returned if a transaction does not spend the entirety of the inputs to the outputs.
	ErrInputOutputSumMismatch = errors.New("inputs and outputs do not spend/deposit the same amount")
	// ErrSignatureAndAddrIncompatible gets returned if an address of an input has a companion signature unlock block with the wrong signature type.
	ErrSignatureAndAddrIncompatible = errors.New("address and signature type are not compatible")
	// ErrInvalidInputUnlock gets returned when an input unlock is invalid.
	ErrInvalidInputUnlock = errors.New("invalid input unlock")
	// ErrSenderFeatureBlockNotUnlocked gets returned when an output contains a SenderFeatureBlock with an ident which is not unlocked.
	ErrSenderFeatureBlockNotUnlocked = errors.New("sender feature block is not unlocked")
	// ErrIssuerFeatureBlockNotUnlocked gets returned when an output contains a IssuerFeatureBlock with an ident which is not unlocked.
	ErrIssuerFeatureBlockNotUnlocked = errors.New("issuer feature block is not unlocked")
	// ErrReturnAmountNotFulFilled gets returned when a return amount in a transaction is not fulfilled by the output side.
	ErrReturnAmountNotFulFilled = errors.New("return amount not fulfilled")
	// ErrTypeIsNotSupportedEssence gets returned when a serializable was found to not be a supported essence.
	ErrTypeIsNotSupportedEssence = errors.New("serializable is not a supported essence")

	txEssenceGuard = serializer.SerializableGuard{
		ReadGuard: func(ty uint32) (serializer.Serializable, error) {
			return TransactionEssenceSelector(ty)
		},
		WriteGuard: func(seri serializer.Serializable) error {
			if seri == nil {
				return fmt.Errorf("%w: because nil", ErrTypeIsNotSupportedEssence)
			}
			if _, is := seri.(*TransactionEssence); !is {
				return fmt.Errorf("%w: because not *TransactionEssence", ErrTypeIsNotSupportedEssence)
			}
			return nil
		},
	}
	txUnlockBlockArrayRules = serializer.ArrayRules{
		// min/max filled out in serialize/deserialize
		Guards: serializer.SerializableGuard{
			ReadGuard:  UnlockBlockSelector,
			WriteGuard: unlockBlockWriteGuard(),
		},
	}
)

// TransactionID is the ID of a Transaction.
type TransactionID = [TransactionIDLength]byte

// TransactionIDs are IDs of transactions.
type TransactionIDs []TransactionID

// Transaction is a transaction with its inputs, outputs and unlock blocks.
type Transaction struct {
	// The transaction essence, respectively the transfer part of a Transaction.
	Essence *TransactionEssence
	// The unlock blocks defining the unlocking data for the inputs within the Essence.
	UnlockBlocks UnlockBlocks
}

func (t *Transaction) PayloadType() PayloadType {
	return PayloadTransaction
}

// OutputsSet returns an OutputSet from the Transaction's outputs, mapped by their OutputID.
func (t *Transaction) OutputsSet() (OutputSet, error) {
	txID, err := t.ID()
	if err != nil {
		return nil, err
	}
	set := make(OutputSet)
	for index, output := range t.Essence.Outputs {
		set[OutputIDFromTransactionIDAndIndex(*txID, uint16(index))] = output
	}
	return set, nil
}

// ID computes the ID of the Transaction.
func (t *Transaction) ID() (*TransactionID, error) {
	data, err := t.Serialize(serializer.DeSeriModeNoValidation, nil)
	if err != nil {
		return nil, fmt.Errorf("can't compute transaction ID: %w", err)
	}
	h := blake2b.Sum256(data)
	return &h, nil
}

func (t *Transaction) Deserialize(data []byte, deSeriMode serializer.DeSerializationMode, deSeriCtx interface{}) (int, error) {
	unlockBlockArrayRulesCopy := txUnlockBlockArrayRules
	return serializer.NewDeserializer(data).
		CheckTypePrefix(uint32(PayloadTransaction), serializer.TypeDenotationUint32, func(err error) error {
			return fmt.Errorf("unable to deserialize transaction: %w", err)
		}).
		ReadObject(&t.Essence, deSeriMode, deSeriCtx, serializer.TypeDenotationByte, txEssenceGuard.ReadGuard, func(err error) error {
			return fmt.Errorf("%w: unable to deserialize transaction essence within transaction", err)
		}).
		Do(func() {
			inputCount := uint(len(t.Essence.Inputs))
			unlockBlockArrayRulesCopy.Min = inputCount
			unlockBlockArrayRulesCopy.Max = inputCount
		}).
		ReadSliceOfObjects(&t.UnlockBlocks, deSeriMode, deSeriCtx, serializer.SeriLengthPrefixTypeAsUint16, serializer.TypeDenotationByte, &unlockBlockArrayRulesCopy, func(err error) error {
			return fmt.Errorf("%w: unable to deserialize unlock blocks", err)
		}).
		WithValidation(deSeriMode, txDeSeriValidation(t, deSeriCtx)).
		Done()
}

func (t *Transaction) Serialize(deSeriMode serializer.DeSerializationMode, deSeriCtx interface{}) ([]byte, error) {
	unlockBlockArrayRulesCopy := txUnlockBlockArrayRules
	inputCount := uint(len(t.Essence.Inputs))
	unlockBlockArrayRulesCopy.Min = inputCount
	unlockBlockArrayRulesCopy.Max = inputCount
	return serializer.NewSerializer().
		WriteNum(PayloadTransaction, func(err error) error {
			return fmt.Errorf("%w: unable to serialize transaction payload ID", err)
		}).
		WriteObject(t.Essence, deSeriMode, deSeriCtx, txEssenceGuard.WriteGuard, func(err error) error {
			return fmt.Errorf("%w: unable to serialize transaction's essence", err)
		}).
		WriteSliceOfObjects(&t.UnlockBlocks, deSeriMode, deSeriCtx, serializer.SeriLengthPrefixTypeAsUint16, &unlockBlockArrayRulesCopy, func(err error) error {
			return fmt.Errorf("%w: unable to serialize transaction's unlock blocks", err)
		}).
		WithValidation(deSeriMode, txDeSeriValidation(t, deSeriCtx)).
		Serialize()
}

func (t *Transaction) MarshalJSON() ([]byte, error) {
	jTransaction := &jsonTransaction{
		UnlockBlocks: make([]*json.RawMessage, len(t.UnlockBlocks)),
	}
	jTransaction.Type = int(PayloadTransaction)
	txJson, err := t.Essence.MarshalJSON()
	if err != nil {
		return nil, err
	}
	rawMsgTxJson := json.RawMessage(txJson)
	jTransaction.Essence = &rawMsgTxJson
	for i, ub := range t.UnlockBlocks {
		jsonUB, err := ub.MarshalJSON()
		if err != nil {
			return nil, err
		}
		rawMsgJsonUB := json.RawMessage(jsonUB)
		jTransaction.UnlockBlocks[i] = &rawMsgJsonUB
	}
	return json.Marshal(jTransaction)
}

func (t *Transaction) UnmarshalJSON(bytes []byte) error {
	jTransaction := &jsonTransaction{}
	if err := json.Unmarshal(bytes, jTransaction); err != nil {
		return err
	}
	seri, err := jTransaction.ToSerializable()
	if err != nil {
		return err
	}
	*t = *seri.(*Transaction)
	return nil
}

func txDeSeriValidation(tx *Transaction, deSeriCtx interface{}) serializer.ErrProducerWithRWBytes {
	return func(readBytes []byte, err error) error {
		deSeriParas, ok := deSeriCtx.(*DeSerializationParameters)
		if !ok {
			return fmt.Errorf("unable to validate transaction: %w", ErrMissingDeSerializationParas)
		}
		return tx.syntacticallyValidate(readBytes, deSeriParas.MinDustDeposit, deSeriParas.RentStructure)
	}
}

// syntacticallyValidate syntactically validates the Transaction.
func (t *Transaction) syntacticallyValidate(readBytes []byte, minDustDep uint64, rentStruct *RentStructure) error {
	if err := t.Essence.syntacticallyValidate(minDustDep, rentStruct); err != nil {
		return fmt.Errorf("transaction essence is invalid: %w", err)
	}

	txID := blake2b.Sum256(readBytes)
	if err := ValidateOutputs(t.Essence.Outputs,
		OutputsSyntacticalAlias(&txID),
		OutputsSyntacticalNFT(&txID),
	); err != nil {
		return err
	}

	if err := ValidateUnlockBlocks(t.UnlockBlocks,
		UnlockBlocksSigUniqueAndRefValidator(),
	); err != nil {
		return fmt.Errorf("invalid unlock blocks: %w", err)
	}

	return nil
}

// SemanticValidationFunc is a function which when called tells whether
// the transaction is passing a specific semantic validation rule or not.
type SemanticValidationFunc = func(t *Transaction, inputs OutputSet) error

// SemanticValidationContext defines the context under which a semantic validation for a Transaction is happening.
type SemanticValidationContext struct {
	// The confirming milestone's index.
	ConfirmingMilestoneIndex uint32
	// The confirming milestone's unix seconds timestamp.
	ConfirmingMilestoneUnix uint64

	// The working set which is auto. populated during the semantic validation.
	WorkingSet *SemValiContextWorkingSet
}

// SemValiContextWorkingSet contains fields which get automatically populated
// by the library during the semantic validation of a Transaction.
type SemValiContextWorkingSet struct {
	// The identities which are successfully unlocked from the input side.
	UnlockedIdents UnlockedIdentities
	// The mapping of OutputID to the actual Outputs.
	InputSet OutputSet
	// The transaction for which this semantic validation happens.
	Tx *Transaction
	// The message which signatures are signing.
	EssenceMsgToSign []byte
	// The inputs of the transaction mapped by type.
	InputsByType OutputsByType
	// The ChainConstrainedOutput(s) at the input side.
	InChains ChainConstrainedOutputsSet
	// The sum of NativeTokens at the input side.
	InNativeTokens NativeTokenSum
	// The Outputs of the transaction mapped by type.
	OutputsByType OutputsByType
	// The ChainConstrainedOutput(s) at the output side.
	OutChains ChainConstrainedOutputsSet
	// The sum of NativeTokens at the output side.
	OutNativeTokens NativeTokenSum
	// The UnlockBlocks carried by the transaction mapped by type.
	UnlockBlocksByType UnlockBlocksByType
}

func featureBlockSetFromOutput(output ChainConstrainedOutput) (FeatureBlocksSet, error) {
	featureBlockOutput, is := output.(FeatureBlockOutput)
	if !is {
		return nil, nil
	}

	featureBlocks, err := featureBlockOutput.FeatureBlocks().Set()
	if err != nil {
		return nil, fmt.Errorf("unable to compute feature block set for output: %w", err)
	}
	return featureBlocks, nil
}

func NewSemValiContextWorkingSet(t *Transaction, inputs OutputSet) (*SemValiContextWorkingSet, error) {
	var err error
	workingSet := &SemValiContextWorkingSet{}
	workingSet.UnlockedIdents = make(UnlockedIdentities)
	workingSet.InputSet = inputs
	workingSet.Tx = t
	workingSet.EssenceMsgToSign, err = t.Essence.SigningMessage()
	if err != nil {
		return nil, err
	}

	workingSet.InputsByType = func() OutputsByType {
		slice := make(Outputs, len(inputs))
		var i int
		for _, output := range inputs {
			slice[i] = output
			i++
		}
		return slice.ToOutputsByType()
	}()

	txID, err := workingSet.Tx.ID()
	if err != nil {
		return nil, err
	}

	workingSet.InChains = workingSet.InputSet.ChainConstrainedOutputSet()
	workingSet.OutputsByType = t.Essence.Outputs.ToOutputsByType()
	workingSet.OutChains = workingSet.Tx.Essence.Outputs.ChainConstrainedOutputSet(*txID)

	workingSet.UnlockBlocksByType = t.UnlockBlocks.ToUnlockBlocksByType()
	return workingSet, nil
}

// SemanticallyValidate semantically validates the Transaction by checking that the semantic rules applied to the inputs
// and outputs are fulfilled. SyntacticallyValidate() should be called before SemanticallyValidate() to
// ensure that the essence part of the transaction is syntactically valid.
func (t *Transaction) SemanticallyValidate(svCtx *SemanticValidationContext, inputs OutputSet, semValFuncs ...SemanticValidationFunc) error {
	var err error
	svCtx.WorkingSet, err = NewSemValiContextWorkingSet(t, inputs)
	if err != nil {
		return err
	}

	// TODO:
	// 	- max 256 native tokens in/out (?)

	// do not change the order of these functions as
	// some of them might depend on certain mutations
	// on the given SemanticValidationContext
	if err := runSemanticValidations(svCtx,
		TxSemanticInputUnlocks(),
		TxSemanticDeposit(),
		TxSemanticNativeTokens(),
		TxSemanticTimelock(),
		TxSemanticOutputsSender(),
		TxSemanticSTVFOnChains()); err != nil {
		return err
	}

	return nil
}

func runSemanticValidations(svCtx *SemanticValidationContext, checks ...TxSemanticValidationFunc) error {
	for _, check := range checks {
		if err := check(svCtx); err != nil {
			return err
		}
	}
	return nil
}

// UnlockedIdentities defines a set of identities which are unlocked from the input side of a Transaction.
type UnlockedIdentities map[string]uint16

// TxSemanticValidationFunc is a function which given the context, input, outputs and
// unlock blocks runs a specific semantic validation. The function might also modify the SemanticValidationContext
// in order to supply information to subsequent TxSemanticValidationFunc(s).
type TxSemanticValidationFunc func(svCtx *SemanticValidationContext) error

// TxSemanticInputUnlocks produces the UnlockedIdentities which will be set into the given SemanticValidationContext
// and verifies that inputs are correctly unlocked.
func TxSemanticInputUnlocks() TxSemanticValidationFunc {
	return func(svCtx *SemanticValidationContext) error {
		// it is important that the inputs are checked in order as referential unlocks
		// check against previous unlocks
		for inputIndex, inputRef := range svCtx.WorkingSet.Tx.Essence.Inputs {
			input, ok := svCtx.WorkingSet.InputSet[inputRef.(IndexedUTXOReferencer).Ref()]
			if !ok {
				return fmt.Errorf("%w: utxo for input %d not supplied", ErrMissingUTXO, inputIndex)
			}

			if err := unlockOutput(svCtx, input, inputIndex); err != nil {
				return err
			}
		}
		return nil
	}
}

func identToUnlock(svCtx *SemanticValidationContext, input Output, inputIndex int) (Address, error) {
	switch in := input.(type) {
	case SingleIdentOutput:
		return in.Ident()
	case MultiIdentOutput:
		return identToUnlockFromMultiIdentOutput(svCtx, in, inputIndex)
	default:
		panic("unknown ident output type in semantic unlocks")
	}
}

// TODO: abstract this all to work with MultiIdentOutput / ChainID instead
func identToUnlockFromMultiIdentOutput(svCtx *SemanticValidationContext, inputMultiIdentOutput MultiIdentOutput, inputIndex int) (Address, error) {
	inputAliasOutput, is := inputMultiIdentOutput.(*AliasOutput)
	if !is {
		// this can not happen because only AliasOutput implements MultiIdentOutput
		panic("non alias output is implementing multi ident output in semantic unlocks")
	}

	aliasID := inputAliasOutput.AliasID
	if aliasID.Empty() {
		aliasID = AliasIDFromOutputID(svCtx.WorkingSet.Tx.Essence.Inputs[inputIndex].(IndexedUTXOReferencer).Ref())
	}

	ident := inputAliasOutput.StateController

	// means a governance transition as either state did not change
	// or the alias output is being destroyed
	if outputAliasOutput, has := svCtx.WorkingSet.OutChains[aliasID]; !has ||
		inputAliasOutput.StateIndex == outputAliasOutput.(*AliasOutput).StateIndex {
		ident = inputAliasOutput.GovernanceController
	}

	return ident, nil
}

func checkSenderFeatureBlockIdentUnlock(svCtx *SemanticValidationContext, output Output) (Address, error) {
	featBlockOutput, is := output.(FeatureBlockOutput)
	if !is {
		return nil, nil
	}

	featBlockSet, err := featBlockOutput.FeatureBlocks().Set()
	if err != nil {
		return nil, err
	}

	featBlockExpMsIndex := featBlockSet[FeatureBlockExpirationMilestoneIndex]
	featBlockExpUnix := featBlockSet[FeatureBlockExpirationUnix]

	if featBlockExpMsIndex == nil && featBlockExpUnix == nil {
		return nil, nil
	}

	// existence guaranteed by syntactical validation
	featBlockSender := featBlockSet[FeatureBlockSender].(*SenderFeatureBlock)

	switch {
	case featBlockExpMsIndex != nil && featBlockExpUnix != nil:
		if featBlockExpMsIndex.(*ExpirationMilestoneIndexFeatureBlock).MilestoneIndex <= svCtx.ConfirmingMilestoneIndex &&
			featBlockExpUnix.(*ExpirationUnixFeatureBlock).UnixTime <= svCtx.ConfirmingMilestoneUnix {
			return featBlockSender.Address, nil
		}
	case featBlockExpMsIndex != nil:
		if featBlockExpMsIndex.(*ExpirationMilestoneIndexFeatureBlock).MilestoneIndex <= svCtx.ConfirmingMilestoneIndex {
			return featBlockSender.Address, nil
		}
	case featBlockExpUnix != nil:
		if featBlockExpUnix.(*ExpirationUnixFeatureBlock).UnixTime <= svCtx.ConfirmingMilestoneUnix {
			return featBlockSender.Address, nil
		}
	}

	return nil, nil
}

func unlockOutput(svCtx *SemanticValidationContext, output Output, inputIndex int) error {
	targetIdent, err := identToUnlock(svCtx, output, inputIndex)
	if err != nil {
		return fmt.Errorf("unable to retrieve ident to unlock of input %d: %w", inputIndex, err)
	}

	actualIdentToUnlock, err := checkSenderFeatureBlockIdentUnlock(svCtx, output)
	if err != nil {
		return err
	}
	if actualIdentToUnlock != nil {
		targetIdent = actualIdentToUnlock
	}

	unlockBlock := svCtx.WorkingSet.Tx.UnlockBlocks[inputIndex]

	switch ident := targetIdent.(type) {
	case ChainConstrainedAddress:
		referentialUnlockBlock, isReferentialUnlockBlock := unlockBlock.(ReferentialUnlockBlock)
		if !isReferentialUnlockBlock || !referentialUnlockBlock.Chainable() || !referentialUnlockBlock.SourceAllowed(targetIdent) {
			return fmt.Errorf("%w: input %d has a chain constrained address of %s but its corresponding unlock block is of type %s", ErrInvalidInputUnlock, inputIndex, AddressTypeToString(ident.Type()), UnlockBlockTypeToString(unlockBlock.Type()))
		}

		unlockedAtIndex, wasUnlocked := svCtx.WorkingSet.UnlockedIdents[ident.Key()]
		if !wasUnlocked || unlockedAtIndex != referentialUnlockBlock.Ref() {
			return fmt.Errorf("%w: input %d's chain constrained address is not unlocked through input %d's unlock block", ErrInvalidInputUnlock, inputIndex, referentialUnlockBlock.Ref())
		}

		// since this input is now unlocked, and it has a ChainConstrainedAddress, it becomes automatically unlocked
		if chainConstrainedOutput, isChainConstrainedOutput := output.(ChainConstrainedOutput); isChainConstrainedOutput && chainConstrainedOutput.Chain().Addressable() {
			svCtx.WorkingSet.UnlockedIdents[chainConstrainedOutput.Chain().ToAddress().Key()] = uint16(inputIndex)
		}

	case DirectUnlockableAddress:
		switch uBlock := unlockBlock.(type) {
		case ReferentialUnlockBlock:
			if uBlock.Chainable() || !uBlock.SourceAllowed(targetIdent) {
				return fmt.Errorf("%w: input %d has none chain constrained address of %s but its corresponding unlock block is of type %s", ErrInvalidInputUnlock, inputIndex, AddressTypeToString(ident.Type()), UnlockBlockTypeToString(unlockBlock.Type()))
			}

			unlockedAtIndex, wasUnlocked := svCtx.WorkingSet.UnlockedIdents[ident.Key()]
			if !wasUnlocked || unlockedAtIndex != uBlock.Ref() {
				return fmt.Errorf("%w: input %d's address is not unlocked through input %d's unlock block", ErrInvalidInputUnlock, inputIndex, uBlock.Ref())
			}
		case *SignatureUnlockBlock:
			// ident must not be unlocked already
			if unlockedAtIndex, wasAlreadyUnlocked := svCtx.WorkingSet.UnlockedIdents[ident.Key()]; wasAlreadyUnlocked {
				return fmt.Errorf("%w: input %d's address is already unlocked through input %d's unlock block but the input uses a non referential unlock block", ErrInvalidInputUnlock, inputIndex, unlockedAtIndex)
			}

			if err := ident.Unlock(svCtx.WorkingSet.EssenceMsgToSign, uBlock.Signature); err != nil {
				return fmt.Errorf("%w: input %d's address is not unlocked through its signature unlock block", err, inputIndex)
			}

			svCtx.WorkingSet.UnlockedIdents[ident.Key()] = uint16(inputIndex)
		}
	default:
		panic("unknown address in semantic unlocks")
	}
	return nil
}

// TxSemanticOutputsSender validates that for SenderFeatureBlock occurring on the output side,
// the given identity is unlocked on the input side.
func TxSemanticOutputsSender() TxSemanticValidationFunc {
	return func(svCtx *SemanticValidationContext) error {
		for outputIndex, output := range svCtx.WorkingSet.Tx.Essence.Outputs {
			featureBlockOutput, is := output.(FeatureBlockOutput)
			if !is {
				continue
			}

			featureBlocks, err := featureBlockOutput.FeatureBlocks().Set()
			if err != nil {
				return fmt.Errorf("unable to compute feature block set for output %d: %w", outputIndex, err)
			}

			senderFeatureBlock, has := featureBlocks[FeatureBlockSender]
			if !has {
				continue
			}

			// check unlocked
			sender := senderFeatureBlock.(*SenderFeatureBlock).Address
			if _, isUnlocked := svCtx.WorkingSet.UnlockedIdents[sender.Key()]; !isUnlocked {
				return fmt.Errorf("%w: output %d", ErrSenderFeatureBlockNotUnlocked, outputIndex)
			}
		}
		return nil
	}
}

// TxSemanticDeposit validates that the IOTA tokens are balanced from the input/output side.
// It additionally also incorporates the check whether return amounts via DustDepositReturnFeatureBlock(s) for specified identities
// are fulfilled from the output side.
func TxSemanticDeposit() TxSemanticValidationFunc {
	return func(svCtx *SemanticValidationContext) error {
		// note that due to syntactic validation of outputs, input and output deposit sums
		// are always within bounds of the total token supply
		var in, out uint64
		inputSumReturnAmountPerIdent := make(map[string]uint64)
		for _, input := range svCtx.WorkingSet.InputSet {
			in += input.Deposit()
			featureBlockOutput, is := input.(FeatureBlockOutput)
			if !is {
				continue
			}
			featBlockSet, err := featureBlockOutput.FeatureBlocks().Set()
			if err != nil {
				return err
			}
			returnFeatBlock, has := featBlockSet[FeatureBlockDustDepositReturn]
			if !has {
				continue
			}
			// guaranteed by syntactical checks
			ident := featBlockSet[FeatureBlockSender].(*SenderFeatureBlock).Address.Key()
			inputSumReturnAmountPerIdent[ident] += returnFeatBlock.(*DustDepositReturnFeatureBlock).Amount
		}

		outputSimpleTransfersPerIdent := make(map[string]uint64)
		for _, output := range svCtx.WorkingSet.Tx.Essence.Outputs {
			outDeposit := output.Deposit()
			out += outDeposit

			// accumulate simple transfers for DustDepositReturnFeatureBlock checks
			switch outputTy := output.(type) {
			case *SimpleOutput:
				outputSimpleTransfersPerIdent[outputTy.Address.Key()] += outDeposit
			case *ExtendedOutput:
				if len(outputTy.FeatureBlocks()) > 0 {
					continue
				}
				outputSimpleTransfersPerIdent[outputTy.Address.Key()] += outDeposit
			}
		}

		if in != out {
			return fmt.Errorf("%w: in %d, out %d", ErrInputOutputSumMismatch, in, out)
		}

		// TODO: augment error with better context
		for ident, returnSum := range inputSumReturnAmountPerIdent {
			outSum, has := outputSimpleTransfersPerIdent[ident]
			if !has {
				return fmt.Errorf("%w: return amount of %d not fulfilled as there is no output for %s", ErrReturnAmountNotFulFilled, returnSum, ident)
			}
			if outSum < returnSum {
				return fmt.Errorf("%w: return amount of %d not fulfilled as output is only %d for %s", ErrReturnAmountNotFulFilled, returnSum, outSum, ident)
			}
		}

		return nil
	}
}

// TxSemanticTimelock validates following rules regarding timelocked inputs:
//	- Inputs with a TimelockMilestone<Index,Unix>FeatureBlock can only be unlocked if the confirming milestone allows it.
func TxSemanticTimelock() TxSemanticValidationFunc {
	return func(svCtx *SemanticValidationContext) error {
		for inputIndex, input := range svCtx.WorkingSet.InputSet {
			inputWithFeatureBlocks, is := input.(FeatureBlockOutput)
			if !is {
				continue
			}
			for _, featureBlock := range inputWithFeatureBlocks.FeatureBlocks() {
				switch block := featureBlock.(type) {
				case *TimelockMilestoneIndexFeatureBlock:
					if svCtx.ConfirmingMilestoneIndex < block.MilestoneIndex {
						return fmt.Errorf("%w: input at index %d's milestone index timelock is not expired, at %d, current %d", ErrInvalidInputUnlock, inputIndex, block.MilestoneIndex, svCtx.ConfirmingMilestoneIndex)
					}
				case *TimelockUnixFeatureBlock:
					if svCtx.ConfirmingMilestoneUnix < block.UnixTime {
						return fmt.Errorf("%w: input at index %d's unix timelock is not expired, at %d, current %d", ErrInvalidInputUnlock, inputIndex, block.UnixTime, svCtx.ConfirmingMilestoneUnix)
					}
				}
			}
		}
		return nil
	}
}

// TxSemanticSTVFOnChains executes StateTransitionValidationFunc(s) on ChainConstrainedOutput(s).
func TxSemanticSTVFOnChains() TxSemanticValidationFunc {
	return func(svCtx *SemanticValidationContext) error {

		for chainID, inputChain := range svCtx.WorkingSet.InChains {
			nextState := svCtx.WorkingSet.OutChains[chainID]
			if nextState == nil {
				if err := inputChain.ValidateStateTransition(ChainTransitionTypeDestroy, nil, svCtx); err != nil {
					return fmt.Errorf("chain input %s state destroy transition failed: %w", chainID, err)
				}
				continue
			}
			if err := inputChain.ValidateStateTransition(ChainTransitionTypeStateChange, nextState, svCtx); err != nil {
				return fmt.Errorf("chain %s state transition failed: %w", chainID, err)
			}
		}

		for chainID, outputChain := range svCtx.WorkingSet.OutChains {
			previousState := svCtx.WorkingSet.InChains[chainID]
			if previousState == nil {
				if err := outputChain.ValidateStateTransition(ChainTransitionTypeGenesis, nil, svCtx); err != nil {
					return fmt.Errorf("new chain %s state transition failed: %w", chainID, err)
				}
			}
		}

		return nil
	}
}

// TxSemanticNativeTokens validates following rules regarding NativeTokens:
//	- The NativeTokens between Inputs / Outputs must be balanced in terms of circulating supply adjustments if
//	  there is no foundry state transition for a given NativeToken.
func TxSemanticNativeTokens() TxSemanticValidationFunc {
	return func(svCtx *SemanticValidationContext) error {
		// native token set creates handle overflows
		var err error
		svCtx.WorkingSet.InNativeTokens, err = svCtx.WorkingSet.InputsByType.NativeTokenOutputs().Sum()
		if err != nil {
			return fmt.Errorf("invalid input native token set: %w", err)
		}

		svCtx.WorkingSet.OutNativeTokens, err = svCtx.WorkingSet.InputsByType.NativeTokenOutputs().Sum()
		if err != nil {
			return fmt.Errorf("invalid output native token set: %w", err)
		}

		// easy route, tokens must be balanced between both sets
		if svCtx.WorkingSet.OutputsByType[OutputFoundry] == nil && svCtx.WorkingSet.InputsByType[OutputFoundry] == nil {
			if err := svCtx.WorkingSet.InNativeTokens.Balanced(svCtx.WorkingSet.OutNativeTokens); err != nil {
				return err
			}
			return nil
		}

		// check for the input and output side whether we have the state transitioning foundry
		// in case either side is missing its companion sum or the tokens are unbalanced by
		// just looking at both sides' sums

		for nativeTokenID, inSum := range svCtx.WorkingSet.InNativeTokens {
			if outSum := svCtx.WorkingSet.OutNativeTokens[nativeTokenID]; outSum == nil || inSum.Cmp(outSum) != 0 {
				if _, foundryIsTransitioning := svCtx.WorkingSet.OutChains[nativeTokenID.FoundryID()]; !foundryIsTransitioning {
					return fmt.Errorf("%w: native token %d exists on input but not output side and the foundry is not transitioning", ErrNativeTokenSumUnbalanced, nativeTokenID)
				}
				continue
			}
		}

		for nativeTokenID, outSum := range svCtx.WorkingSet.OutNativeTokens {
			if inSum := svCtx.WorkingSet.InNativeTokens[nativeTokenID]; inSum == nil || inSum.Cmp(outSum) != 0 {
				if _, foundryIsTransitioning := svCtx.WorkingSet.OutChains[nativeTokenID.FoundryID()]; !foundryIsTransitioning {
					return fmt.Errorf("%w: native token %d exists on output but not input side and the foundry is not transitioning", ErrNativeTokenSumUnbalanced, nativeTokenID)
				}
				continue
			}
		}

		// from here the native tokens balancing is handled by the foundry's STVF

		return nil
	}
}

// jsonTransaction defines the json representation of a Transaction.
type jsonTransaction struct {
	Type         int                `json:"type"`
	Essence      *json.RawMessage   `json:"essence"`
	UnlockBlocks []*json.RawMessage `json:"unlockBlocks"`
}

func (jsontx *jsonTransaction) ToSerializable() (serializer.Serializable, error) {
	jsonTxEssence, err := DeserializeObjectFromJSON(jsontx.Essence, jsonTransactionEssenceSelector)
	if err != nil {
		return nil, fmt.Errorf("unable to decode transaction essence from JSON: %w", err)
	}

	txEssenceSeri, err := jsonTxEssence.ToSerializable()
	if err != nil {
		return nil, err
	}

	unlockBlocks, err := unlockBlocksFromJSONRawMsg(jsontx.UnlockBlocks)
	if err != nil {
		return nil, err
	}

	return &Transaction{Essence: txEssenceSeri.(*TransactionEssence), UnlockBlocks: unlockBlocks}, nil
}
