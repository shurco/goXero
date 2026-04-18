package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/shurco/goxero/internal/models"
)

type ItemRepository struct {
	pool *pgxpool.Pool
}

const itemColumns = `
	item_id, code, COALESCE(name,''), COALESCE(description,''), COALESCE(purchase_description,''),
	is_tracked_as_inventory, is_sold, is_purchased,
	COALESCE(inventory_asset_account_code,''), quantity_on_hand,
	sales_unit_price, COALESCE(sales_account_code,''), COALESCE(sales_tax_type,''),
	purchase_unit_price, COALESCE(purchase_account_code,''), COALESCE(purchase_tax_type,''),
	total_cost_pool, updated_date_utc`

func scanItem(row pgx.Row) (*models.Item, error) {
	var (
		it              models.Item
		salesPrice      *decimal.Decimal
		salesAccount    string
		salesTaxType    string
		purchasePrice   *decimal.Decimal
		purchaseAccount string
		purchaseTaxType string
	)
	err := row.Scan(
		&it.ItemID, &it.Code, &it.Name, &it.Description, &it.PurchaseDescription,
		&it.IsTrackedAsInventory, &it.IsSold, &it.IsPurchased,
		&it.InventoryAssetAccountCode, &it.QuantityOnHand,
		&salesPrice, &salesAccount, &salesTaxType,
		&purchasePrice, &purchaseAccount, &purchaseTaxType,
		&it.TotalCostPool, &it.UpdatedDateUTC,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if salesPrice != nil || salesAccount != "" || salesTaxType != "" {
		it.SalesDetails = &models.ItemPriceDetails{
			UnitPrice: salesPrice, AccountCode: salesAccount, TaxType: salesTaxType,
		}
	}
	if purchasePrice != nil || purchaseAccount != "" || purchaseTaxType != "" {
		it.PurchaseDetails = &models.ItemPriceDetails{
			UnitPrice: purchasePrice, AccountCode: purchaseAccount, TaxType: purchaseTaxType,
		}
	}
	return &it, nil
}

func (r *ItemRepository) List(ctx context.Context, orgID uuid.UUID) ([]models.Item, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT "+itemColumns+" FROM items WHERE organisation_id=$1 ORDER BY code", orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []models.Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *it)
	}
	return list, rows.Err()
}

func (r *ItemRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*models.Item, error) {
	return scanItem(r.pool.QueryRow(ctx,
		"SELECT "+itemColumns+" FROM items WHERE organisation_id=$1 AND item_id=$2", orgID, id))
}

func (r *ItemRepository) Create(ctx context.Context, orgID uuid.UUID, it *models.Item) error {
	var salesPrice, purchasePrice *decimal.Decimal
	var salesAcc, salesTax, purchAcc, purchTax string
	if it.SalesDetails != nil {
		salesPrice = it.SalesDetails.UnitPrice
		salesAcc = it.SalesDetails.AccountCode
		salesTax = it.SalesDetails.TaxType
	}
	if it.PurchaseDetails != nil {
		purchasePrice = it.PurchaseDetails.UnitPrice
		purchAcc = it.PurchaseDetails.AccountCode
		purchTax = it.PurchaseDetails.TaxType
	}
	q := `INSERT INTO items (
		organisation_id, code, name, description, purchase_description,
		is_tracked_as_inventory, is_sold, is_purchased, inventory_asset_account_code,
		sales_unit_price, sales_account_code, sales_tax_type,
		purchase_unit_price, purchase_account_code, purchase_tax_type
	) VALUES ($1,$2,NULLIF($3,''),NULLIF($4,''),NULLIF($5,''),
	          $6,$7,$8, NULLIF($9,''),
	          $10, NULLIF($11,''), NULLIF($12,''),
	          $13, NULLIF($14,''), NULLIF($15,''))
	RETURNING item_id, updated_date_utc`
	return r.pool.QueryRow(ctx, q,
		orgID, it.Code, it.Name, it.Description, it.PurchaseDescription,
		it.IsTrackedAsInventory, it.IsSold, it.IsPurchased, it.InventoryAssetAccountCode,
		salesPrice, salesAcc, salesTax,
		purchasePrice, purchAcc, purchTax,
	).Scan(&it.ItemID, &it.UpdatedDateUTC)
}

func (r *ItemRepository) Update(ctx context.Context, orgID uuid.UUID, it *models.Item) error {
	var salesPrice, purchasePrice *decimal.Decimal
	var salesAcc, salesTax, purchAcc, purchTax string
	if it.SalesDetails != nil {
		salesPrice = it.SalesDetails.UnitPrice
		salesAcc = it.SalesDetails.AccountCode
		salesTax = it.SalesDetails.TaxType
	}
	if it.PurchaseDetails != nil {
		purchasePrice = it.PurchaseDetails.UnitPrice
		purchAcc = it.PurchaseDetails.AccountCode
		purchTax = it.PurchaseDetails.TaxType
	}
	q := `UPDATE items SET
		code=$3, name=NULLIF($4,''), description=NULLIF($5,''), purchase_description=NULLIF($6,''),
		is_tracked_as_inventory=$7, is_sold=$8, is_purchased=$9,
		inventory_asset_account_code=NULLIF($10,''),
		sales_unit_price=$11, sales_account_code=NULLIF($12,''), sales_tax_type=NULLIF($13,''),
		purchase_unit_price=$14, purchase_account_code=NULLIF($15,''), purchase_tax_type=NULLIF($16,''),
		updated_date_utc=now()
		WHERE organisation_id=$1 AND item_id=$2
		RETURNING updated_date_utc`
	err := r.pool.QueryRow(ctx, q,
		orgID, it.ItemID,
		it.Code, it.Name, it.Description, it.PurchaseDescription,
		it.IsTrackedAsInventory, it.IsSold, it.IsPurchased, it.InventoryAssetAccountCode,
		salesPrice, salesAcc, salesTax,
		purchasePrice, purchAcc, purchTax,
	).Scan(&it.UpdatedDateUTC)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func (r *ItemRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	cmd, err := r.pool.Exec(ctx,
		`DELETE FROM items WHERE organisation_id=$1 AND item_id=$2`, orgID, id)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
