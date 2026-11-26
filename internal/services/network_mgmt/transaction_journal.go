package network_mgmt

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// TransactionState represents valid stages in a network staging transaction lifecycle.
type TransactionState string

const (
	TxPreflightPassed   TransactionState = "PREFLIGHT_PASSED"
	TxSnapshotPersisted TransactionState = "SNAPSHOT_PERSISTED"
	TxSnapshotValidated TransactionState = "SNAPSHOT_VALIDATED"
	TxOperatorApproved  TransactionState = "OPERATOR_APPROVED"
	TxApplyStarted      TransactionState = "APPLY_STARTED"
	TxApplyVerified     TransactionState = "APPLY_VERIFIED"
	TxCommitted         TransactionState = "COMMITTED"
	TxRollbackRequired  TransactionState = "ROLLBACK_REQUIRED"
	TxRollbackStarted   TransactionState = "ROLLBACK_STARTED"
	TxRollbackVerified  TransactionState = "ROLLBACK_VERIFIED"
	TxRecoveryFailed    TransactionState = "RECOVERY_FAILED"
)

// ValidStateTransitions defines the strict state transition graph.
var ValidStateTransitions = map[TransactionState][]TransactionState{
	TxPreflightPassed:   {TxSnapshotPersisted, TxRollbackRequired},
	TxSnapshotPersisted: {TxSnapshotValidated, TxRollbackRequired},
	TxSnapshotValidated: {TxOperatorApproved, TxRollbackRequired},
	TxOperatorApproved:  {TxApplyStarted, TxRollbackRequired},
	TxApplyStarted:      {TxApplyVerified, TxRollbackRequired, TxRollbackStarted},
	TxApplyVerified:     {TxCommitted, TxRollbackRequired},
	TxCommitted:         {TxRollbackRequired}, // Can initiate rollback even after commit if operator requests
	TxRollbackRequired:  {TxRollbackStarted, TxRecoveryFailed},
	TxRollbackStarted:   {TxRollbackVerified, TxRecoveryFailed},
	TxRollbackVerified:  {}, // Terminal successful recovery
	TxRecoveryFailed:    {}, // Terminal failure requiring manual operator intervention
}

// TransactionTransition records an individual state change event.
type TransactionTransition struct {
	FromState TransactionState `json:"from_state"`
	ToState   TransactionState `json:"to_state"`
	Timestamp time.Time        `json:"timestamp"`
	Details   string           `json:"details,omitempty"`
	Error     string           `json:"error,omitempty"`
}

// NetworkTransaction is the durable record of a network staging transaction.
type NetworkTransaction struct {
	TransactionID    string                  `json:"transaction_id"`
	CurrentState     TransactionState        `json:"current_state"`
	CreatedAt        time.Time               `json:"created_at"`
	UpdatedAt        time.Time               `json:"updated_at"`
	ProfileID        string                  `json:"profile_id"`
	TargetAdapterKey string                  `json:"target_adapter_key"`
	TargetMAC        string                  `json:"target_mac"`
	TargetAlias      string                  `json:"target_alias"`
	SnapshotID       string                  `json:"snapshot_id"`
	SnapshotPath     string                  `json:"snapshot_path"`
	SnapshotHash     string                  `json:"snapshot_hash"`
	OperatorApprover string                  `json:"operator_approver,omitempty"`
	OperatorNote     string                  `json:"operator_note,omitempty"`
	ErrorMessage     string                  `json:"error_message,omitempty"`
	History          []TransactionTransition `json:"history"`
	Interrupted      bool                    `json:"interrupted"`
	RequiresReview   bool                    `json:"requires_review"`
	ReviewNote       string                  `json:"review_note,omitempty"`
}

// TransactionJournal manages persistent transaction logs with atomic disk commits.
type TransactionJournal struct {
	journalDir string
	mu         sync.RWMutex
}

// NewTransactionJournal initializes or connects to the durable transaction journal directory.
func NewTransactionJournal(storageRoot string) (*TransactionJournal, error) {
	if storageRoot == "" {
		storageRoot = `C:\NKB-PoC\`
	}
	journalDir := filepath.Join(storageRoot, "config", "transactions")
	if err := os.MkdirAll(journalDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create transaction journal directory: %w", err)
	}

	return &TransactionJournal{
		journalDir: journalDir,
	}, nil
}

// JournalDir returns the transaction log storage directory.
func (j *TransactionJournal) JournalDir() string {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.journalDir
}

// BeginTransaction initiates a new durable transaction in PREFLIGHT_PASSED state.
func (j *TransactionJournal) BeginTransaction(p *NetworkProfile, target *NetworkAdapter, snap *WindowsAdapterSnapshot) (*NetworkTransaction, error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	// Check if another active unfinished transaction exists
	active, _ := j.getActiveTransactionLocked()
	if active != nil && !isTerminalState(active.CurrentState) && !active.Interrupted {
		return nil, fmt.Errorf("cannot begin new transaction: active transaction %s is already in progress (%s)",
			active.TransactionID, active.CurrentState)
	}

	randBytes := make([]byte, 4)
	_, _ = rand.Read(randBytes)
	txID := fmt.Sprintf("tx_%s_%s", time.Now().UTC().Format("20060102_150405"), hex.EncodeToString(randBytes))

	now := time.Now().UTC()
	tx := &NetworkTransaction{
		TransactionID:    txID,
		CurrentState:     TxPreflightPassed,
		CreatedAt:        now,
		UpdatedAt:        now,
		ProfileID:        p.ProfileID,
		TargetAdapterKey: target.ID,
		TargetMAC:        target.MACAddress,
		TargetAlias:      target.InterfaceAlias,
		History: []TransactionTransition{
			{
				FromState: "",
				ToState:   TxPreflightPassed,
				Timestamp: now,
				Details:   fmt.Sprintf("Preflight validation passed for %s (%s)", target.InterfaceAlias, target.MACAddress),
			},
		},
	}

	if snap != nil {
		tx.SnapshotID = snap.SnapshotID
		tx.SnapshotPath = filepath.Join("config", "snapshots", fmt.Sprintf("%s.json", snap.SnapshotID))
		tx.SnapshotHash = snap.IntegrityHash
	}

	if err := j.persistTransactionLocked(tx); err != nil {
		return nil, err
	}

	return tx, nil
}

// Transition moves a transaction to a new state if the transition is valid according to the state machine.
func (j *TransactionJournal) Transition(txID string, nextState TransactionState, details string, opErr error) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	tx, err := j.loadTransactionLocked(txID)
	if err != nil {
		return err
	}

	if tx.Interrupted && tx.RequiresReview {
		return fmt.Errorf("transaction %s is marked INTERRUPTED; operator review required before resuming", txID)
	}

	// Validate transition graph
	allowed, exists := ValidStateTransitions[tx.CurrentState]
	if !exists {
		return fmt.Errorf("invalid current state: %s", tx.CurrentState)
	}

	valid := false
	for _, a := range allowed {
		if a == nextState {
			valid = true
			break
		}
	}

	// Recovery transitions can always move to ROLLBACK_REQUIRED or RECOVERY_FAILED on error
	if !valid && (nextState == TxRollbackRequired || nextState == TxRecoveryFailed) {
		valid = true
	}

	if !valid {
		return fmt.Errorf("illegal state transition from %s to %s for transaction %s", tx.CurrentState, nextState, txID)
	}

	now := time.Now().UTC()
	errStr := ""
	if opErr != nil {
		errStr = opErr.Error()
		tx.ErrorMessage = errStr
	}

	tx.History = append(tx.History, TransactionTransition{
		FromState: tx.CurrentState,
		ToState:   nextState,
		Timestamp: now,
		Details:   details,
		Error:     errStr,
	})

	tx.CurrentState = nextState
	tx.UpdatedAt = now

	return j.persistTransactionLocked(tx)
}

// Approve records explicit operator authorization for a transaction.
func (j *TransactionJournal) Approve(txID, approver, note string) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	tx, err := j.loadTransactionLocked(txID)
	if err != nil {
		return err
	}

	if tx.CurrentState != TxSnapshotValidated {
		return fmt.Errorf("transaction %s is in state %s; operator approval requires SNAPSHOT_VALIDATED", txID, tx.CurrentState)
	}

	now := time.Now().UTC()
	tx.OperatorApprover = approver
	tx.OperatorNote = note
	tx.CurrentState = TxOperatorApproved
	tx.UpdatedAt = now
	tx.History = append(tx.History, TransactionTransition{
		FromState: TxSnapshotValidated,
		ToState:   TxOperatorApproved,
		Timestamp: now,
		Details:   fmt.Sprintf("Authorized by %s: %s", approver, note),
	})

	return j.persistTransactionLocked(tx)
}

// LoadTransaction reads a transaction record from disk by ID.
func (j *TransactionJournal) LoadTransaction(txID string) (*NetworkTransaction, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.loadTransactionLocked(txID)
}

func (j *TransactionJournal) loadTransactionLocked(txID string) (*NetworkTransaction, error) {
	filePath := filepath.Join(j.journalDir, fmt.Sprintf("%s.json", txID))
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("transaction %s not found: %w", txID, err)
	}

	var tx NetworkTransaction
	if err := json.Unmarshal(data, &tx); err != nil {
		return nil, fmt.Errorf("transaction %s corrupted: %w", txID, err)
	}
	return &tx, nil
}

// GetActiveTransaction returns the latest in-progress transaction, if any.
func (j *TransactionJournal) GetActiveTransaction() (*NetworkTransaction, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.getActiveTransactionLocked()
}

func (j *TransactionJournal) getActiveTransactionLocked() (*NetworkTransaction, error) {
	list, err := j.listTransactionsLocked()
	if err != nil || len(list) == 0 {
		return nil, nil
	}

	for _, tx := range list {
		if !isTerminalState(tx.CurrentState) {
			return tx, nil
		}
	}
	return nil, nil
}

// DetectInterruptedTransactions inspects journal logs on restart to discover
// transactions that terminated mid-flight without reaching a terminal committed or rolled-back state.
// Marks them as Interrupted: true and RequiresReview: true to prevent silent resume.
func (j *TransactionJournal) DetectInterruptedTransactions() ([]*NetworkTransaction, error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	list, err := j.listTransactionsLocked()
	if err != nil {
		return nil, err
	}

	var interrupted []*NetworkTransaction
	for _, tx := range list {
		if !isTerminalState(tx.CurrentState) && !tx.Interrupted {
			// Found in-flight non-terminal transaction after restart
			tx.Interrupted = true
			tx.RequiresReview = true
			tx.ErrorMessage = fmt.Sprintf("Process termination or interruption detected while in %s", tx.CurrentState)
			tx.UpdatedAt = time.Now().UTC()
			tx.History = append(tx.History, TransactionTransition{
				FromState: tx.CurrentState,
				ToState:   tx.CurrentState,
				Timestamp: tx.UpdatedAt,
				Details:   "SYSTEM RESTART DETECTED: Transaction marked interrupted to prevent silent mutation",
				Error:     tx.ErrorMessage,
			})
			_ = j.persistTransactionLocked(tx)
			interrupted = append(interrupted, tx)
		} else if tx.Interrupted && tx.RequiresReview {
			interrupted = append(interrupted, tx)
		}
	}

	return interrupted, nil
}

// AcknowledgeInterruption allows an operator to review and resolve an interrupted transaction.
func (j *TransactionJournal) AcknowledgeInterruption(txID, reviewNote string, markTerminal bool) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	tx, err := j.loadTransactionLocked(txID)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	tx.ReviewNote = reviewNote
	tx.RequiresReview = false
	tx.UpdatedAt = now

	if markTerminal {
		tx.CurrentState = TxRecoveryFailed
		tx.History = append(tx.History, TransactionTransition{
			FromState: tx.CurrentState,
			ToState:   TxRecoveryFailed,
			Timestamp: now,
			Details:   fmt.Sprintf("Operator reviewed interrupted transaction and closed as RECOVERY_FAILED: %s", reviewNote),
		})
	} else {
		tx.History = append(tx.History, TransactionTransition{
			FromState: tx.CurrentState,
			ToState:   tx.CurrentState,
			Timestamp: now,
			Details:   fmt.Sprintf("Operator reviewed interrupted transaction: %s", reviewNote),
		})
	}

	return j.persistTransactionLocked(tx)
}

func (j *TransactionJournal) listTransactionsLocked() ([]*NetworkTransaction, error) {
	entries, err := os.ReadDir(j.journalDir)
	if err != nil {
		return nil, err
	}

	var list []*NetworkTransaction
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "tx_") && strings.HasSuffix(e.Name(), ".json") {
			data, err := os.ReadFile(filepath.Join(j.journalDir, e.Name()))
			if err != nil {
				continue
			}
			var tx NetworkTransaction
			if err := json.Unmarshal(data, &tx); err == nil {
				list = append(list, &tx)
			}
		}
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt.After(list[j].CreatedAt)
	})

	return list, nil
}

// persistTransactionLocked writes the transaction atomically via temp file rename.
func (j *TransactionJournal) persistTransactionLocked(tx *NetworkTransaction) error {
	data, err := json.MarshalIndent(tx, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal transaction %s: %w", tx.TransactionID, err)
	}

	targetPath := filepath.Join(j.journalDir, fmt.Sprintf("%s.json", tx.TransactionID))
	tmpPath := targetPath + ".tmp"

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write transaction tmp file: %w", err)
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		return fmt.Errorf("failed to atomically commit transaction file: %w", err)
	}

	return nil
}

func isTerminalState(s TransactionState) bool {
	return s == TxCommitted || s == TxRollbackVerified || s == TxRecoveryFailed
}
