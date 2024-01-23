package tree

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/vulpemventures/go-elements/elementsutil"
	"github.com/vulpemventures/go-elements/psetv2"
	"github.com/vulpemventures/go-elements/taproot"
	"github.com/vulpemventures/go-elements/transaction"
)

var (
	ErrInvalidPoolTransaction   = errors.New("invalid pool transaction")
	ErrEmptyTree                = errors.New("empty congestion tree")
	ErrInvalidRootLevel         = errors.New("root level must have only one node")
	ErrNoLeaves                 = errors.New("no leaves in the tree")
	ErrNodeTransactionEmpty     = errors.New("node transaction is empty")
	ErrNodeTxidEmpty            = errors.New("node txid is empty")
	ErrNodeParentTxidEmpty      = errors.New("node parent txid is empty")
	ErrNodeTxidDifferent        = errors.New("node txid differs from node transaction")
	ErrNumberOfInputs           = errors.New("node transaction should have only one input")
	ErrNumberOfOutputs          = errors.New("node transaction should have only three or two outputs")
	ErrParentTxidInput          = errors.New("parent txid should be the input of the node transaction")
	ErrNumberOfChildren         = errors.New("node branch transaction should have two children")
	ErrLeafChildren             = errors.New("leaf node should have max 1 child")
	ErrInvalidChildTxid         = errors.New("invalid child txid")
	ErrNumberOfTapscripts       = errors.New("input should have two tapscripts leaves")
	ErrInternalKey              = errors.New("taproot internal key is not unspendable")
	ErrInvalidTaprootScript     = errors.New("invalid taproot script")
	ErrInvalidLeafTaprootScript = errors.New("invalid leaf taproot script")
	ErrInvalidAmount            = errors.New("children amount is different from parent amount")
	ErrInvalidAsset             = errors.New("invalid output asset")
	ErrInvalidSweepSequence     = errors.New("invalid sweep sequence")
	ErrInvalidASP               = errors.New("invalid ASP")
	ErrMissingFeeOutput         = errors.New("missing fee output")
	ErrInvalidLeftOutput        = errors.New("invalid left output")
	ErrInvalidRightOutput       = errors.New("invalid right output")
	ErrMissingSweepTapscript    = errors.New("missing sweep tapscript")
	ErrMissingBranchTapscript   = errors.New("missing branch tapscript")
	ErrInvalidLeaf              = errors.New("leaf node shouldn't have children")
	ErrWrongPoolTxID            = errors.New("root input should be the pool tx outpoint")
)

const (
	UnspendablePoint  = "0250929b74c1a04954b78b4b6035e97a5e078a5a0f28ec96d547bfee9ace803ac0"
	sharedOutputIndex = 0
)

// ValidateCongestionTree checks if the given congestion tree is valid
// poolTxID & poolTxIndex & poolTxAmount are used to validate the root input outpoint
// aspPublicKey & roundLifetimeSeconds are used to validate the sweep tapscript leaves
// besides that, the function validates:
// - the number of nodes
// - the number of leaves
// - children coherence with parent
// - every control block and taproot output scripts
// - input and output amounts
func ValidateCongestionTree(
	tree CongestionTree,
	poolTxHex string,
	aspPublicKey *secp256k1.PublicKey,
	roundLifetimeSeconds uint,
) error {
	unspendableKeyBytes, _ := hex.DecodeString(UnspendablePoint)
	unspendableKey, _ := secp256k1.ParsePubKey(unspendableKeyBytes)

	poolTransaction, err := transaction.NewTxFromHex(poolTxHex)
	if err != nil {
		return ErrInvalidPoolTransaction
	}

	poolTxAmount, err := elementsutil.ValueFromBytes(poolTransaction.Outputs[sharedOutputIndex].Value)
	if err != nil {
		return ErrInvalidPoolTransaction
	}

	poolTxID := poolTransaction.TxHash().String()

	nbNodes := tree.NumberOfNodes()
	if nbNodes == 0 {
		return ErrEmptyTree
	}

	if len(tree[0]) != 1 {
		return ErrInvalidRootLevel
	}

	// check that root input is connected to the pool tx
	rootPsetB64 := tree[0][0].Tx
	rootPset, err := psetv2.NewPsetFromBase64(rootPsetB64)
	if err != nil {
		return fmt.Errorf("invalid root transaction: %w", err)
	}

	if len(rootPset.Inputs) != 1 {
		return ErrNumberOfInputs
	}

	rootInput := rootPset.Inputs[0]
	if chainhash.Hash(rootInput.PreviousTxid).String() != poolTxID || rootInput.PreviousTxIndex != sharedOutputIndex {
		return ErrWrongPoolTxID
	}

	sumRootValue := uint64(0)
	for _, output := range rootPset.Outputs {
		sumRootValue += output.Value
	}

	if sumRootValue != poolTxAmount {
		return ErrInvalidAmount
	}

	if len(tree.Leaves()) == 0 {
		return ErrNoLeaves
	}

	// iterates over all the nodes of the tree
	for _, level := range tree {
		for _, node := range level {
			if err := validateNodeTransaction(node, tree, unspendableKey, aspPublicKey, roundLifetimeSeconds); err != nil {
				return err
			}
		}
	}

	return nil
}

func validateNodeTransaction(
	node Node,
	tree CongestionTree,
	expectedInternalKey,
	expectedPublicKeyASP *secp256k1.PublicKey,
	expectedSequenceSeconds uint,
) error {
	if node.Tx == "" {
		return ErrNodeTransactionEmpty
	}

	if node.Txid == "" {
		return ErrNodeTxidEmpty
	}

	if node.ParentTxid == "" {
		return ErrNodeParentTxidEmpty
	}

	decodedPset, err := psetv2.NewPsetFromBase64(node.Tx)
	if err != nil {
		return fmt.Errorf("invalid node transaction: %w", err)
	}

	utx, err := decodedPset.UnsignedTx()
	if err != nil {
		return fmt.Errorf("invalid node transaction: %w", err)
	}

	if utx.TxHash().String() != node.Txid {
		return ErrNodeTxidDifferent
	}

	if len(decodedPset.Inputs) != 1 {
		return ErrNumberOfInputs
	}

	input := decodedPset.Inputs[0]
	if len(input.TapLeafScript) != 2 {
		return ErrNumberOfTapscripts
	}

	if chainhash.Hash(decodedPset.Inputs[0].PreviousTxid).String() != node.ParentTxid {
		return ErrParentTxidInput
	}

	feeOutput := decodedPset.Outputs[len(decodedPset.Outputs)-1]
	if len(feeOutput.Script) != 0 {
		return ErrMissingFeeOutput
	}

	children := tree.Children(node.Txid)

	if node.Leaf && len(children) > 1 {
		return ErrLeafChildren
	}

	for childIndex, child := range children {
		childTx, err := psetv2.NewPsetFromBase64(child.Tx)
		if err != nil {
			return fmt.Errorf("invalid child transaction: %w", err)
		}

		parentOutput := decodedPset.Outputs[childIndex]
		previousScriptKey := parentOutput.Script[2:]
		if len(previousScriptKey) != 32 {
			return ErrInvalidTaprootScript
		}

		sweepLeafFound := false
		branchLeafFound := false

		for _, tapLeaf := range childTx.Inputs[0].TapLeafScript {
			key := tapLeaf.ControlBlock.InternalKey
			if !key.IsEqual(expectedInternalKey) {
				return ErrInternalKey
			}

			rootHash := tapLeaf.ControlBlock.RootHash(tapLeaf.Script)
			outputScript := taproot.ComputeTaprootOutputKey(key, rootHash)

			if !bytes.Equal(schnorr.SerializePubKey(outputScript), previousScriptKey) {
				return ErrInvalidTaprootScript
			}

			isSweepLeaf, aspKey, seconds, err := decodeSweepScript(tapLeaf.Script)
			if err != nil {
				return fmt.Errorf("invalid sweep script: %w", err)
			}

			if isSweepLeaf {
				if !aspKey.IsEqual(aspKey) {
					return ErrInvalidASP
				}

				if seconds != expectedSequenceSeconds {
					return ErrInvalidSweepSequence
				}

				sweepLeafFound = true
				continue
			}

			isBranchLeaf, leftKey, rightKey, leftAmount, rightAmount, err := decodeBranchScript(tapLeaf.Script)
			if err != nil {
				return fmt.Errorf("invalid vtxo script: %w", err)
			}

			if isBranchLeaf {
				branchLeafFound = true

				// check outputs
				nbOuts := len(childTx.Outputs)
				if leftKey != nil && rightKey != nil {
					if nbOuts != 3 {
						return ErrNumberOfOutputs
					}
				} else {
					if nbOuts != 2 {
						return ErrNumberOfOutputs
					}
				}

				leftWitnessProgram := childTx.Outputs[0].Script[2:]
				leftOutputAmount := childTx.Outputs[0].Value

				if !bytes.Equal(leftWitnessProgram, schnorr.SerializePubKey(leftKey)) {
					return ErrInvalidLeftOutput
				}

				if leftAmount != leftOutputAmount {
					return ErrInvalidLeftOutput
				}

				if rightKey != nil {
					rightWitnessProgram := childTx.Outputs[1].Script[2:]
					rightOutputAmount := childTx.Outputs[1].Value

					if !bytes.Equal(rightWitnessProgram, schnorr.SerializePubKey(rightKey)) {
						return ErrInvalidRightOutput
					}

					if rightAmount != rightOutputAmount {
						return ErrInvalidRightOutput
					}
				}
			}
		}

		if !sweepLeafFound {
			return ErrMissingSweepTapscript
		}

		if !branchLeafFound {
			return ErrMissingBranchTapscript
		}

		sumChildAmount := uint64(0)
		for _, output := range childTx.Outputs {
			sumChildAmount += output.Value
			if !bytes.Equal(output.Asset, parentOutput.Asset) {
				return ErrInvalidAsset
			}
		}

		if sumChildAmount != parentOutput.Value {
			return ErrInvalidAmount
		}
	}

	return nil
}