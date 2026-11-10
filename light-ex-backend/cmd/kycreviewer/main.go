// Command kycreviewer grants or revokes KYC reviewer access for an existing account.
//
// Reviewer access is an operator action: the API has no administrator role, so
// grants are not exposed over HTTP.
//
//	go run ./cmd/kycreviewer grant  user@example.com
//	go run ./cmd/kycreviewer revoke user@example.com
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/light-group/light-ex-backend/internal/modules/kyc/repository"
	"github.com/light-group/light-ex-backend/internal/modules/kyc/repository/postgres"
	"github.com/light-group/light-ex-backend/internal/modules/kyc/service"
	"github.com/light-group/light-ex-backend/internal/platform/bootstrap"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "kycreviewer:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 2 || (args[0] != "grant" && args[0] != "revoke") {
		return errors.New("usage: kycreviewer grant|revoke <email>")
	}
	app, err := bootstrap.Start()
	if err != nil {
		return err
	}
	defer app.Close()

	svc := service.New(postgres.New(app.Database.SQL()))
	ctx := context.Background()
	var id any
	switch args[0] {
	case "grant":
		id, err = svc.GrantReviewer(ctx, args[1])
	case "revoke":
		id, err = svc.RevokeReviewer(ctx, args[1])
	}
	if errors.Is(err, repository.ErrUserNotFound) {
		return fmt.Errorf("no account with email %q", args[1])
	}
	if err != nil {
		return err
	}
	fmt.Printf("%s: user %v\n", args[0], id)
	return nil
}
