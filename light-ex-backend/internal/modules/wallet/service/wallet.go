package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/light-group/light-ex-backend/internal/modules/wallet/domain"
	"github.com/light-group/light-ex-backend/internal/modules/wallet/repository"
	"github.com/light-group/light-ex-backend/internal/modules/wallet/repository/postgres"
)

type Service struct {
	repo repository.Repository
}

// New creates a new wallet service instance.
func New(repo repository.Repository) *Service {
	return &Service{
		repo: repo,
	}
}

// CreateWallet provisions a new wallet for a user with the specified currency.
func (s *Service) CreateWallet(
	ctx context.Context,
	userID uuid.UUID,
	currency string,
) (*domain.Wallet, error) {
	if userID == uuid.Nil {
		return nil, ErrInvalidUserID
	}

	exists, err := s.repo.ExistsByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrWalletAlreadyExists
	}

	wallet, err := domain.NewWallet(userID, currency)
	if err != nil {
		return nil, err
	}

	if err := s.repo.Create(ctx, wallet); err != nil {
		return nil, err
	}

	return wallet, nil
}

// GetWalletByID retrieves a wallet by its ID.
func (s *Service) GetWalletByID(
	ctx context.Context,
	walletID uuid.UUID,
) (*domain.Wallet, error) {
	wallet, err := s.repo.FindByID(ctx, walletID)
	if err != nil {
		if errors.Is(err, postgres.ErrWalletNotFound) {
			return nil, ErrWalletNotFound
		}
		return nil, err
	}
	return wallet, nil
}

// GetWalletByUserID retrieves a wallet associated with a user.
func (s *Service) GetWalletByUserID(
	ctx context.Context,
	userID uuid.UUID,
) (*domain.Wallet, error) {
	wallet, err := s.repo.FindByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, postgres.ErrWalletNotFound) {
			return nil, ErrWalletNotFound
		}
		return nil, err
	}
	return wallet, nil
}

// GetOrCreateWallet retrieves an existing wallet or creates one if none exists.
func (s *Service) GetOrCreateWallet(
	ctx context.Context,
	userID uuid.UUID,
	currency string,
) (*domain.Wallet, error) {
	wallet, err := s.GetWalletByUserID(ctx, userID)
	if err == nil {
		return wallet, nil
	}

	if !errors.Is(err, ErrWalletNotFound) {
		return nil, err
	}

	return s.CreateWallet(ctx, userID, currency)
}

// CreditBalance adds funds to a wallet's available balance.
func (s *Service) CreditBalance(
	ctx context.Context,
	walletID uuid.UUID,
	amount decimal.Decimal,
) (*domain.Wallet, error) {
	wallet, err := s.GetWalletByID(ctx, walletID)
	if err != nil {
		return nil, err
	}

	if err := wallet.Credit(amount); err != nil {
		return nil, err
	}

	if err := s.repo.Update(ctx, wallet); err != nil {
		return nil, err
	}

	return wallet, nil
}

// DebitBalance removes funds from a wallet's available balance.
func (s *Service) DebitBalance(
	ctx context.Context,
	walletID uuid.UUID,
	amount decimal.Decimal,
) (*domain.Wallet, error) {
	wallet, err := s.GetWalletByID(ctx, walletID)
	if err != nil {
		return nil, err
	}

	if err := wallet.Debit(amount); err != nil {
		return nil, err
	}

	if err := s.repo.Update(ctx, wallet); err != nil {
		return nil, err
	}

	return wallet, nil
}

// LockBalance transfers funds from available to locked balance.
func (s *Service) LockBalance(
	ctx context.Context,
	walletID uuid.UUID,
	amount decimal.Decimal,
) (*domain.Wallet, error) {
	wallet, err := s.GetWalletByID(ctx, walletID)
	if err != nil {
		return nil, err
	}

	if err := wallet.LockFunds(amount); err != nil {
		return nil, err
	}

	if err := s.repo.Update(ctx, wallet); err != nil {
		return nil, err
	}

	return wallet, nil
}

// UnlockBalance transfers funds from locked back to available balance.
func (s *Service) UnlockBalance(
	ctx context.Context,
	walletID uuid.UUID,
	amount decimal.Decimal,
) (*domain.Wallet, error) {
	wallet, err := s.GetWalletByID(ctx, walletID)
	if err != nil {
		return nil, err
	}

	if err := wallet.UnlockFunds(amount); err != nil {
		return nil, err
	}

	if err := s.repo.Update(ctx, wallet); err != nil {
		return nil, err
	}

	return wallet, nil
}
