package cep_repository

import (
	"errors"

	"vozko/domain/cep"
	"vozko/infra/database/schema"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const upsertSQL = `INSERT INTO ceps (id, cep, logradouro, complement, bairro, localidade, uf, ibge, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, now(), now())
ON CONFLICT (cep) DO UPDATE SET
	logradouro = EXCLUDED.logradouro,
	complement = EXCLUDED.complement,
	bairro = EXCLUDED.bairro,
	localidade = EXCLUDED.localidade,
	uf = EXCLUDED.uf,
	ibge = EXCLUDED.ibge,
	updated_at = now(),
	deleted_at = NULL`

const markCheckedSQL = `UPDATE ceps SET updated_at = now() WHERE cep = ? AND deleted_at IS NULL`

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) cep.CEPRepository {
	return &repository{db: db}
}

func (r *repository) GetByCode(cepCode string) (*cep.CEPInfo, error) {
	var dbCEP schema.CEP
	if err := r.db.Where("cep = ?", cepCode).First(&dbCEP).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	info := &cep.CEPInfo{
		Cep:        dbCEP.Cep,
		Logradouro: dbCEP.Logradouro,
		Complement: dbCEP.Complement,
		Bairro:     dbCEP.Bairro,
		Localidade: dbCEP.Localidade,
		Uf:         dbCEP.Uf,
		CheckedAt:  dbCEP.UpdatedAt,
	}
	if dbCEP.IBGE != nil {
		info.IBGE = *dbCEP.IBGE
	}
	return info, nil
}

func (r *repository) Save(info *cep.CEPInfo) error {
	var cityCode *string
	if info.IBGE != "" {
		cityCode = &info.IBGE
	}
	return r.db.Exec(upsertSQL,
		uuid.New().String(), info.Cep, info.Logradouro, info.Complement,
		info.Bairro, info.Localidade, info.Uf, cityCode,
	).Error
}

func (r *repository) MarkChecked(cepCode string) error {
	return r.db.Exec(markCheckedSQL, cepCode).Error
}
