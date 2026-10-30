package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	paymentdomain "github.com/light-group/light-ex-backend/internal/modules/payment/domain"
	"github.com/light-group/light-ex-backend/internal/modules/payment/provider"
)

// fakeInitiator records the last request it received and answers with the
// configured result or error.
type fakeInitiator struct {
	result provider.Result
	err    error
	calls  int
	last   provider.Request
}

func (f *fakeInitiator) Collect(_ context.Context, req provider.Request) (provider.Result, error) {
	f.calls++
	f.last = req
	return f.result, f.err
}

func (f *fakeInitiator) Disburse(_ context.Context, req provider.Request) (provider.Result, error) {
	f.calls++
	f.last = req
	return f.result, f.err
}

func baseCollectionInput(userID, walletID uuid.UUID) CollectionInput {
	return CollectionInput{
		DepositInput: DepositInput{
			UserID:   userID,
			WalletID: walletID,
			Method:   "MTN_MOMO",
			Amount:   decimal.NewFromInt(30),
			Currency: "USD",
		},
		Phone: "233240000000",
	}
}

func TestInitiateDeposit_PersistsProcessingWithProviderReference(t *testing.T) {
	db := getDepositTestDB(t)
	userID, walletID := seedUserWallet(t, db, "100")
	ctx := context.Background()

	fake := &fakeInitiator{result: provider.Result{ProviderReference: "prov-ref-1", Status: provider.StatusPending}}
	collector := NewCollector(NewSQLDepositStore(db), fake)

	res, err := collector.InitiateDeposit(ctx, baseCollectionInput(userID, walletID))
	if err != nil {
		t.Fatalf("InitiateDeposit: %v", err)
	}
	if fake.calls != 1 {
		t.Fatalf("expected one provider call, got %d", fake.calls)
	}
	if res.Payment.Status != paymentdomain.StatusProcessing {
		t.Fatalf("expected PROCESSING payment, got %s", res.Payment.Status)
	}
	if res.Payment.ProviderReference != "prov-ref-1" || res.ProviderReference != "prov-ref-1" {
		t.Fatalf("provider reference not saved: %+v", res.Payment)
	}
	if res.Payment.Reference == "" {
		t.Fatal("expected generated payment reference")
	}
	// Initiation must not move balances.
	if got := readBalance(t, db, walletID); !got.Equal(decimal.NewFromInt(100)) {
		t.Fatalf("balance changed at initiation: %s", got)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM transactions WHERE wallet_id = $1`, walletID); n != 0 {
		t.Fatalf("no transaction should exist before settlement, got %d", n)
	}
}

func TestInitiateDeposit_ProviderFailureMarksPaymentFailed(t *testing.T) {
	db := getDepositTestDB(t)
	userID, walletID := seedUserWallet(t, db, "100")
	ctx := context.Background()

	fake := &fakeInitiator{err: errors.New("sandbox unavailable")}
	collector := NewCollector(NewSQLDepositStore(db), fake)

	_, err := collector.InitiateDeposit(ctx, baseCollectionInput(userID, walletID))
	if err == nil {
		t.Fatal("expected error from failing provider")
	}
	var status string
	reference := fake.last.Reference
	if err := db.QueryRowContext(ctx, `SELECT status FROM payments WHERE reference = $1`, reference).Scan(&status); err != nil {
		t.Fatalf("read payment: %v", err)
	}
	if status != "FAILED" {
		t.Fatalf("expected FAILED payment after provider failure, got %s", status)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM payments WHERE wallet_id = $1 AND status = 'FAILED'`, walletID); n != 1 {
		t.Fatalf("expected exactly one FAILED payment, got %d", n)
	}
}

func TestInitiateDeposit_MissingProviderReferenceFails(t *testing.T) {
	db := getDepositTestDB(t)
	userID, walletID := seedUserWallet(t, db, "100")
	ctx := context.Background()

	fake := &fakeInitiator{result: provider.Result{ProviderReference: " ", Status: provider.StatusPending}}
	collector := NewCollector(NewSQLDepositStore(db), fake)

	_, err := collector.InitiateDeposit(ctx, baseCollectionInput(userID, walletID))
	if !errors.Is(err, ErrEmptyProviderReference) {
		t.Fatalf("expected ErrEmptyProviderReference, got %v", err)
	}
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM payments WHERE reference = $1`, fake.last.Reference).Scan(&status); err != nil {
		t.Fatalf("read payment: %v", err)
	}
	if status != "FAILED" {
		t.Fatalf("expected FAILED payment when no provider reference returned, got %s", status)
	}
}

func TestInitiateDeposit_RejectsBadWalletBeforeProviderCall(t *testing.T) {
	db := getDepositTestDB(t)
	userID, _ := seedUserWallet(t, db, "100")
	ctx := context.Background()

	fake := &fakeInitiator{result: provider.Result{ProviderReference: "prov-ref-1", Status: provider.StatusPending}}
	collector := NewCollector(NewSQLDepositStore(db), fake)

	otherWallet := uuid.New()
	in := baseCollectionInput(userID, otherWallet)
	in.Phone = ""
	if _, err := collector.InitiateDeposit(ctx, in); err == nil {
		t.Fatal("expected error for a wallet owned by nobody")
	}
	if fake.calls != 0 {
		t.Fatalf("provider must not be called for a rejected request, got %d calls", fake.calls)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM payments WHERE user_id = $1`, userID); n != 0 {
		t.Fatalf("no payment should be recorded for a bad request, got %d", n)
	}
}

func TestInitiateDeposit_CompletedReferenceReplaysWithoutProviderCall(t *testing.T) {
	db := getDepositTestDB(t)
	userID, walletID := seedUserWallet(t, db, "100")
	ctx := context.Background()

	in := baseCollectionInput(userID, walletID)
	in.Reference = "ASYNC-" + uuid.NewString()

	// First pass completes the whole request synchronously by settling it as a
	// deposit with the same reference (any completed deposit must replay).
	depositor := NewDepositor(NewSQLDepositStore(db))
	if _, err := depositor.Deposit(ctx, in.DepositInput); err != nil {
		t.Fatalf("seed completed deposit: %v", err)
	}

	fake := &fakeInitiator{result: provider.Result{ProviderReference: "prov-ref-2", Status: provider.StatusPending}}
	res, err := NewCollector(NewSQLDepositStore(db), fake).InitiateDeposit(ctx, in)
	if err != nil {
		t.Fatalf("replay InitiateDeposit: %v", err)
	}
	if fake.calls != 0 {
		t.Fatalf("provider must not be called for a completed reference, got %d", fake.calls)
	}
	if res.Payment.Status != paymentdomain.StatusCompleted {
		t.Fatalf("expected replay of COMPLETED payment, got %s", res.Payment.Status)
	}
}

func readLockedBalance(t *testing.T, db *sql.DB, walletID uuid.UUID) decimal.Decimal {
	t.Helper()

	var locked decimal.Decimal
	if err := db.QueryRowContext(context.Background(),
		`SELECT locked_balance FROM wallets WHERE id = $1`, walletID).Scan(&locked); err != nil {
		t.Fatalf("read locked balance: %v", err)
	}
	return locked
}

func seedWithdrawalInput(userID, walletID uuid.UUID) CollectionInput {
	in := baseCollectionInput(userID, walletID)
	in.Reference = "WTHD-" + uuid.NewString()
	in.Description = "test withdrawal"
	return in
}

func TestInitiateWithdrawal_ReservesFundsAndPersistsRequest(t *testing.T) {
	db := getDepositTestDB(t)
	userID, walletID := seedUserWallet(t, db, "100")
	ctx := context.Background()

	fake := &fakeInitiator{result: provider.Result{ProviderReference: "wprov-ref-1", Status: provider.StatusPending}}
	res, err := NewCollector(NewSQLDepositStore(db), fake).InitiateWithdrawal(ctx, seedWithdrawalInput(userID, walletID))
	if err != nil {
		t.Fatalf("InitiateWithdrawal: %v", err)
	}
	if fake.calls != 1 {
		t.Fatalf("expected one provider call, got %d", fake.calls)
	}
	if res.Payment.Status != paymentdomain.StatusProcessing {
		t.Fatalf("expected PROCESSING payment, got %s", res.Payment.Status)
	}
	if res.Payment.ProviderReference != "wprov-ref-1" {
		t.Fatalf("provider reference not saved: %+v", res.Payment)
	}
	if got, locked := readBalance(t, db, walletID), readLockedBalance(t, db, walletID); !got.Equal(decimal.NewFromInt(70)) || !locked.Equal(decimal.NewFromInt(30)) {
		t.Fatalf("reservation wrong: available=%s locked=%s", got, locked)
	}
	var status, wtype string
	if err := db.QueryRowContext(ctx, `SELECT status, type FROM transactions WHERE reference = $1`, res.Payment.Reference).Scan(&status, &wtype); err != nil {
		t.Fatalf("outbound transaction missing: %v", err)
	}
	if status != "PENDING" || wtype != "WITHDRAWAL" {
		t.Fatalf("outbound transaction wrong: status=%s type=%s", status, wtype)
	}
}

func TestInitiateWithdrawal_InsufficientFundsFailsAtomically(t *testing.T) {
	db := getDepositTestDB(t)
	userID, walletID := seedUserWallet(t, db, "100")
	ctx := context.Background()

	fake := &fakeInitiator{result: provider.Result{ProviderReference: "wprov-ref-1", Status: provider.StatusPending}}
	in := seedWithdrawalInput(userID, walletID)
	in.Amount = decimal.NewFromInt(200)
	if _, err := NewCollector(NewSQLDepositStore(db), fake).InitiateWithdrawal(ctx, in); err == nil {
		t.Fatal("expected insufficient funds error")
	}
	if fake.calls != 0 {
		t.Fatalf("provider must not be called, got %d calls", fake.calls)
	}
	if got, locked := readBalance(t, db, walletID), readLockedBalance(t, db, walletID); !got.Equal(decimal.NewFromInt(100)) || !locked.Equal(decimal.NewFromInt(0)) {
		t.Fatalf("balances changed: available=%s locked=%s", got, locked)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM payments WHERE wallet_id = $1`, walletID); n != 0 {
		t.Fatalf("payment left behind: %d", n)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM transactions WHERE wallet_id = $1`, walletID); n != 0 {
		t.Fatalf("transaction left behind: %d", n)
	}
}

func TestInitiateWithdrawal_ProviderFailureReleasesFunds(t *testing.T) {
	db := getDepositTestDB(t)
	userID, walletID := seedUserWallet(t, db, "100")
	ctx := context.Background()

	fake := &fakeInitiator{err: errors.New("sandbox unavailable")}
	in := seedWithdrawalInput(userID, walletID)
	if _, err := NewCollector(NewSQLDepositStore(db), fake).InitiateWithdrawal(ctx, in); err == nil {
		t.Fatal("expected error from failing provider")
	}
	if got, locked := readBalance(t, db, walletID), readLockedBalance(t, db, walletID); !got.Equal(decimal.NewFromInt(100)) || !locked.Equal(decimal.NewFromInt(0)) {
		t.Fatalf("reservation not released: available=%s locked=%s", got, locked)
	}
	var pstatus, tstatus string
	if err := db.QueryRowContext(ctx, `SELECT status FROM payments WHERE reference = $1`, fake.last.Reference).Scan(&pstatus); err != nil {
		t.Fatalf("read payment: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT status FROM transactions WHERE reference = $1`, fake.last.Reference).Scan(&tstatus); err != nil {
		t.Fatalf("read transaction: %v", err)
	}
	if pstatus != "FAILED" || tstatus != "FAILED" {
		t.Fatalf("expected FAILED payment and transaction, got %s and %s", pstatus, tstatus)
	}
}

func TestInitiateWithdrawal_MissingProviderReferenceReleasesFunds(t *testing.T) {
	db := getDepositTestDB(t)
	userID, walletID := seedUserWallet(t, db, "100")
	ctx := context.Background()

	fake := &fakeInitiator{result: provider.Result{ProviderReference: " ", Status: provider.StatusPending}}
	in := seedWithdrawalInput(userID, walletID)
	if _, err := NewCollector(NewSQLDepositStore(db), fake).InitiateWithdrawal(ctx, in); !errors.Is(err, ErrEmptyProviderReference) {
		t.Fatalf("expected ErrEmptyProviderReference, got %v", err)
	}
	if got, locked := readBalance(t, db, walletID), readLockedBalance(t, db, walletID); !got.Equal(decimal.NewFromInt(100)) || !locked.Equal(decimal.NewFromInt(0)) {
		t.Fatalf("reservation not released: available=%s locked=%s", got, locked)
	}
	var pstatus string
	if err := db.QueryRowContext(ctx, `SELECT status FROM payments WHERE reference = $1`, fake.last.Reference).Scan(&pstatus); err != nil {
		t.Fatalf("read payment: %v", err)
	}
	if pstatus != "FAILED" {
		t.Fatalf("expected FAILED payment, got %s", pstatus)
	}
}

func TestInitiateWithdrawal_CompletedReferenceReplaysWithoutProviderCall(t *testing.T) {
	db := getDepositTestDB(t)
	userID, walletID := seedUserWallet(t, db, "100")
	ctx := context.Background()

	// A settled withdrawal reservation (locked funds already debited)
	in := seedWithdrawalInput(userID, walletID)
	if _, err := NewWithdrawer(NewSQLDepositStore(db)).Withdraw(ctx, in.DepositInput); err != nil {
		t.Fatalf("seed completed withdrawal: %v", err)
	}

	fake := &fakeInitiator{result: provider.Result{ProviderReference: "wprov-ref-2", Status: provider.StatusPending}}
	res, err := NewCollector(NewSQLDepositStore(db), fake).InitiateWithdrawal(ctx, in)
	if err != nil {
		t.Fatalf("replay InitiateWithdrawal: %v", err)
	}
	if fake.calls != 0 {
		t.Fatalf("provider must not be called for a completed reference, got %d", fake.calls)
	}
	if res.Payment.Status != paymentdomain.StatusCompleted {
		t.Fatalf("expected replay of COMPLETED payment, got %s", res.Payment.Status)
	}
}
