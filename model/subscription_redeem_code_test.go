package model

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func setupSubscriptionRedeemCodeTestDB(t *testing.T) {
	t.Helper()
	previousDB, previousLogDB := DB, LOG_DB
	previousMainDatabaseType, previousLogDatabaseType := common.MainDatabaseType(), common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	recorder := &migrationSQLRecorder{}
	config := &gorm.Config{Logger: recorder}
	var db *gorm.DB
	databaseType := common.DatabaseTypeSQLite
	switch strings.ToLower(strings.TrimSpace(os.Getenv("APIC_REDEEM_TEST_DB"))) {
	case "mysql":
		databaseType = common.DatabaseTypeMySQL
		dsn := strings.TrimSpace(os.Getenv("TEST_MYSQL_DSN"))
		require.NotEmpty(t, dsn, "TEST_MYSQL_DSN is required when APIC_REDEEM_TEST_DB=mysql")
		var err error
		db, err = gorm.Open(mysql.Open(dsn), config)
		require.NoError(t, err)
	case "postgres":
		databaseType = common.DatabaseTypePostgreSQL
		dsn := strings.TrimSpace(os.Getenv("TEST_POSTGRES_DSN"))
		require.NotEmpty(t, dsn, "TEST_POSTGRES_DSN is required when APIC_REDEEM_TEST_DB=postgres")
		var err error
		db, err = gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), config)
		require.NoError(t, err)
	default:
		dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
		var err error
		db, err = gorm.Open(sqlite.Open(dsn), config)
		require.NoError(t, err)
	}
	common.SetDatabaseTypes(databaseType, databaseType)
	DB, LOG_DB = db, db
	require.NoError(t, db.Migrator().DropTable(&SubscriptionRedeemCode{}, &UserSubscription{}, &SubscriptionPlan{}, &User{}))
	require.NoError(t, db.AutoMigrate(&User{}, &SubscriptionPlan{}, &UserSubscription{}, &SubscriptionRedeemCode{}))
	recorder.reset()
	require.NoError(t, db.AutoMigrate(&User{}, &SubscriptionPlan{}, &UserSubscription{}, &SubscriptionRedeemCode{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(8)
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		_ = sqlDB.Close()
	})
}

func seedSubscriptionRedeemPlanAndUser(t *testing.T) (*User, *SubscriptionPlan) {
	t.Helper()
	user := &User{
		Username:    fmt.Sprintf("redeem-%d", time.Now().UnixNano()),
		Password:    "unused-password-hash",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AuthVersion: 1,
		Quota:       77,
	}
	require.NoError(t, DB.Create(user).Error)
	plan := &SubscriptionPlan{
		Title:            "Test day card",
		DurationUnit:     SubscriptionDurationDay,
		DurationValue:    1,
		TotalAmount:      1000,
		QuotaResetPeriod: SubscriptionResetDaily,
		Enabled:          true,
	}
	require.NoError(t, DB.Create(plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)
	return user, plan
}

func TestSubscriptionRedeemCodeCreatesSubscriptionWithoutChangingBalance(t *testing.T) {
	setupSubscriptionRedeemCodeTestDB(t)
	user, plan := seedSubscriptionRedeemPlanAndUser(t)
	_, issued, err := GenerateSubscriptionRedeemCodes(plan.Id, 1, 0, 9001)
	require.NoError(t, err)
	require.Len(t, issued, 1)
	var stored SubscriptionRedeemCode
	require.NoError(t, DB.First(&stored).Error)
	require.Len(t, stored.CodeHash, 64)
	require.NotContains(t, stored.CodeHash, issued[0].Code)

	sub, err := RedeemSubscriptionCode(issued[0].Code, user.Id)
	require.NoError(t, err)
	require.Equal(t, user.Id, sub.UserId)
	require.Equal(t, plan.Id, sub.PlanId)
	require.Equal(t, int64(1000), sub.AmountTotal)
	require.Equal(t, "active", sub.Status)
	require.Equal(t, "redeem_code", sub.Source)

	var after User
	require.NoError(t, DB.First(&after, user.Id).Error)
	require.Equal(t, user.Quota, after.Quota, "subscription redemption must not credit wallet balance")
	stored = SubscriptionRedeemCode{}
	require.NoError(t, DB.First(&stored).Error)
	require.Equal(t, SubscriptionRedeemCodeRedeemed, stored.Status)
	require.Equal(t, user.Id, stored.RedeemedBy)
	require.Equal(t, sub.Id, stored.SubscriptionId)

	_, err = RedeemSubscriptionCode(issued[0].Code, user.Id)
	require.ErrorIs(t, err, ErrSubscriptionRedeemCodeInvalid)
	var count int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", user.Id).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestSubscriptionRedeemCodeRejectsExpiredAndUnknownCodes(t *testing.T) {
	setupSubscriptionRedeemCodeTestDB(t)
	user, plan := seedSubscriptionRedeemPlanAndUser(t)
	_, issued, err := GenerateSubscriptionRedeemCodes(plan.Id, 1, 0, 9001)
	require.NoError(t, err)
	var stored SubscriptionRedeemCode
	require.NoError(t, DB.First(&stored).Error)
	require.NoError(t, DB.Model(&stored).Update("expires_at", common.GetTimestamp()-1).Error)

	_, err = RedeemSubscriptionCode(issued[0].Code, user.Id)
	require.ErrorIs(t, err, ErrSubscriptionRedeemCodeInvalid)
	_, err = RedeemSubscriptionCode("APIC-NOT-A-REAL-CODE", user.Id)
	require.ErrorIs(t, err, ErrSubscriptionRedeemCodeInvalid)
	var count int64
	require.NoError(t, DB.Model(&UserSubscription{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestConcurrentSubscriptionRedeemCodeCreatesAtMostOneSubscription(t *testing.T) {
	setupSubscriptionRedeemCodeTestDB(t)
	user, plan := seedSubscriptionRedeemPlanAndUser(t)
	_, issued, err := GenerateSubscriptionRedeemCodes(plan.Id, 1, 0, 9001)
	require.NoError(t, err)
	var successes int
	var successesMu sync.Mutex
	var start sync.WaitGroup
	start.Add(1)
	var done sync.WaitGroup
	for range 8 {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			_, redeemErr := RedeemSubscriptionCode(issued[0].Code, user.Id)
			if redeemErr == nil {
				successesMu.Lock()
				successes++
				successesMu.Unlock()
			}
		}()
	}
	start.Done()
	done.Wait()
	var count int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", user.Id).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.Equal(t, 1, successes)
	var code SubscriptionRedeemCode
	require.NoError(t, DB.First(&code).Error)
	require.Equal(t, SubscriptionRedeemCodeRedeemed, code.Status)
}

func TestSubscriptionRedeemCodeBatchBoundsAndExpiryValidation(t *testing.T) {
	setupSubscriptionRedeemCodeTestDB(t)
	_, plan := seedSubscriptionRedeemPlanAndUser(t)
	_, _, err := GenerateSubscriptionRedeemCodes(plan.Id, MaxSubscriptionRedeemBatchSize+1, 0, 9001)
	require.Error(t, err)
	_, _, err = GenerateSubscriptionRedeemCodes(plan.Id, 1, common.GetTimestamp(), 9001)
	require.Error(t, err)
	_, _, err = GenerateSubscriptionRedeemCodes(plan.Id+500, 1, 0, 9001)
	require.Error(t, err)
}

func TestSubscriptionPlanExternalPurchaseValidation(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		currency  string
		price     float64
		wantError bool
		wantCode  string
	}{
		{name: "empty link defaults currency", currency: "", wantCode: "CNY"},
		{name: "valid https link normalizes currency", url: " https://shop.example/item/123 ", currency: " cny ", wantCode: "CNY"},
		{name: "http link is rejected", url: "http://shop.example/item/123", currency: "CNY", wantError: true},
		{name: "script link is rejected", url: "javascript:alert(1)", currency: "CNY", wantError: true},
		{name: "userinfo link is rejected", url: "https://user:pass@shop.example/item/123", currency: "CNY", wantError: true},
		{name: "invalid currency is rejected", currency: "CN1", wantError: true},
		{name: "negative price is rejected", currency: "CNY", price: -1, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := &SubscriptionPlan{
				ExternalPurchaseURL:      test.url,
				ExternalPurchasePrice:    test.price,
				ExternalPurchaseCurrency: test.currency,
			}
			err := plan.ValidateAndNormalizeExternalPurchase()
			if test.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantCode, plan.ExternalPurchaseCurrency)
			require.NotContains(t, plan.ExternalPurchaseURL, " ")
		})
	}
}

type legacySubscriptionPlanForRedeemMigration struct {
	Id                      int     `gorm:"primaryKey"`
	Title                   string  `gorm:"type:varchar(128);not null"`
	Subtitle                string  `gorm:"type:varchar(255);default:''"`
	PriceAmount             float64 `gorm:"type:decimal(10,6);not null;default:0"`
	Currency                string  `gorm:"type:varchar(8);not null;default:'USD'"`
	DurationUnit            string  `gorm:"type:varchar(16);not null;default:'month'"`
	DurationValue           int     `gorm:"type:int;not null;default:1"`
	CustomSeconds           int64   `gorm:"type:bigint;not null;default:0"`
	Enabled                 bool    `gorm:"default:true"`
	SortOrder               int     `gorm:"type:int;default:0"`
	AllowBalancePay         *bool
	AllowWalletOverflow     *bool
	StripePriceId           string `gorm:"type:varchar(128);default:''"`
	CreemProductId          string `gorm:"type:varchar(128);default:''"`
	WaffoPancakeProductId   string `gorm:"type:varchar(128);default:''"`
	MaxPurchasePerUser      int    `gorm:"type:int;default:0"`
	UpgradeGroup            string `gorm:"type:varchar(64);default:''"`
	DowngradeGroup          string `gorm:"type:varchar(64);default:''"`
	TotalAmount             int64  `gorm:"type:bigint;not null;default:0"`
	QuotaResetPeriod        string `gorm:"type:varchar(16);default:'never'"`
	QuotaResetCustomSeconds int64  `gorm:"type:bigint;default:0"`
	CreatedAt               int64  `gorm:"bigint"`
	UpdatedAt               int64  `gorm:"bigint"`
}

func (legacySubscriptionPlanForRedeemMigration) TableName() string { return "subscription_plans" }

func migrateSubscriptionRedeemTestSchema(t *testing.T) {
	t.Helper()
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		require.NoError(t, ensureSubscriptionPlanTableSQLite())
	}
	require.NoError(t, DB.AutoMigrate(&SubscriptionPlan{}, &SubscriptionRedeemCode{}))
}
func TestSubscriptionRedeemMigrationUpgradesExistingPlanWithoutDataLoss(t *testing.T) {
	setupSubscriptionRedeemCodeTestDB(t)
	require.NoError(t, DB.Migrator().DropTable(&SubscriptionRedeemCode{}, &SubscriptionPlan{}))
	legacyPlan := &legacySubscriptionPlanForRedeemMigration{
		Id: 301, Title: "Existing weekly plan", PriceAmount: 25, Currency: "USD",
		DurationUnit: SubscriptionDurationDay, DurationValue: 7, TotalAmount: 45000,
		QuotaResetPeriod: SubscriptionResetDaily,
	}
	require.NoError(t, DB.AutoMigrate(&legacySubscriptionPlanForRedeemMigration{}))
	require.NoError(t, DB.Create(legacyPlan).Error)
	migrateSubscriptionRedeemTestSchema(t)

	var migratedPlan SubscriptionPlan
	require.NoError(t, DB.First(&migratedPlan, legacyPlan.Id).Error)
	require.Equal(t, legacyPlan.Title, migratedPlan.Title)
	require.Equal(t, legacyPlan.PriceAmount, migratedPlan.PriceAmount)
	require.Equal(t, legacyPlan.TotalAmount, migratedPlan.TotalAmount)
	require.Equal(t, "", migratedPlan.ExternalPurchaseCurrency)
	require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("id = ?", legacyPlan.Id).Updates(map[string]any{
		"external_purchase_url":      "https://shop.example/day",
		"external_purchase_price":    4.5,
		"external_purchase_currency": "CNY",
	}).Error)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPlan{}, &SubscriptionRedeemCode{}))
	var migratedAgain SubscriptionPlan
	require.NoError(t, DB.First(&migratedAgain, legacyPlan.Id).Error)
	require.Equal(t, "https://shop.example/day", migratedAgain.ExternalPurchaseURL)
	require.Equal(t, 4.5, migratedAgain.ExternalPurchasePrice)
	require.Equal(t, "CNY", migratedAgain.ExternalPurchaseCurrency)
	require.True(t, DB.Migrator().HasColumn(&SubscriptionPlan{}, "external_purchase_url"))
	require.True(t, DB.Migrator().HasColumn(&SubscriptionPlan{}, "external_purchase_price"))
	require.True(t, DB.Migrator().HasColumn(&SubscriptionPlan{}, "external_purchase_currency"))

	firstCode := SubscriptionRedeemCode{
		CodeHash: strings.Repeat("a", 64), PlanId: migratedPlan.Id, BatchId: strings.Repeat("b", 32),
		CreatedBy: 9001, Status: SubscriptionRedeemCodeIssued, CreatedAt: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(&firstCode).Error)
	duplicateCode := firstCode
	duplicateCode.Id = 0
	require.Error(t, DB.Create(&duplicateCode).Error, "the migrated voucher digest index must remain unique")
}
