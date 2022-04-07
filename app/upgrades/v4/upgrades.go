package v4

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
	gravitytypes "github.com/peggyjv/gravity-bridge/module/x/gravity/types"
	cellarfeestypes "github.com/peggyjv/sommelier/v4/x/cellarfees/types"
	corktypes "github.com/peggyjv/sommelier/v4/x/cork/types"
)

func CreateUpgradeHandler(
	mm *module.Manager,
	configurator module.Configurator,
	bankKeeper bankkeeper.Keeper,
) upgradetypes.UpgradeHandler {
	fmt.Println("v4 upgrade: entering handler")
	return func(ctx sdk.Context, plan upgradetypes.Plan, vm module.VersionMap) (module.VersionMap, error) {
		// Since this is the first in-place upgrade and InitChainer was not set up for this at genesis
		// time, we must initialize the VM map ourselves.
		fromVM := make(map[string]uint64)
		for moduleName, module := range mm.Modules {
			// we skip setting the consensus version for the new modules so that
			// RunMigrations will call InitGenesis on them
			if moduleName != cellarfeestypes.ModuleName && moduleName != corktypes.ModuleName {
				fromVM[moduleName] = module.ConsensusVersion()
			}
		}

		// Overwrite the gravity module's version back to 1 so the migration will run to v2
		fromVM[gravitytypes.ModuleName] = 1

		ctx.Logger().Info("v4 upgrade: normalizing gravity denoms in bank balances")
		normalizeGravityDenoms(ctx, bankKeeper)

		ctx.Logger().Info("v4 upgrade: running migrations and exiting handler")
		return mm.RunMigrations(ctx, configurator, fromVM)
	}
}

func normalizeGravityDenoms(ctx sdk.Context, bankKeeper bankkeeper.Keeper) {
	// Make a mapping of all existing, incorrect gravity denoms to their
	// normalized versions
	denomsToRepair := make(map[string]string)
	bankKeeper.IterateTotalSupply(ctx, func(supply sdk.Coin) bool {
		normalizedDenom := gravitytypes.NormalizeDenom(supply.Denom)

		if normalizedDenom != supply.Denom {
			denomsToRepair[supply.Denom] = normalizedDenom
		}

		return false
	})

	// If any account's balance appears in the list of denoms we have to normalize,
	// transfer the coins to the gravity module, burn them, mint new coins with the new
	// denom, and send them back to the account
	bankKeeper.IterateAllBalances(ctx, func(addr sdk.AccAddress, coin sdk.Coin) bool {
		if normalizedDenom, ok := denomsToRepair[coin.Denom]; ok {
			oldCoins := sdk.NewCoins(coin)

			if err := bankKeeper.SendCoinsFromAccountToModule(ctx, addr, gravitytypes.ModuleName, oldCoins); err != nil {
				panic(err)
			}
			if err := bankKeeper.BurnCoins(ctx, gravitytypes.ModuleName, oldCoins); err != nil {
				panic(err)
			}

			normalizedCoins := sdk.NewCoins(sdk.NewCoin(normalizedDenom, coin.Amount))

			if err := bankKeeper.MintCoins(ctx, gravitytypes.ModuleName, normalizedCoins); err != nil {
				panic(err)
			}
			if err := bankKeeper.SendCoinsFromModuleToAccount(ctx, gravitytypes.ModuleName, addr, normalizedCoins); err != nil {
				panic(err)
			}

		}

		return false
	})
}
