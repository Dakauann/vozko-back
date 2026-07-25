package cep_repository

import (
	"vozko/domain/cep"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) cep.CEPRepository {
	return &repository{db: db}
}

func (r *repository) GetByCode(cepCode string) (*cep.CEPInfo, error) {
	var dbCEP schema.CEP
	if err := r.db.Where("cep = ?", cepCode).First(&dbCEP).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return &cep.CEPInfo{
		Cep:        dbCEP.Cep,
		Logradouro: dbCEP.Logradouro,
		Complement: dbCEP.Complement,
		Bairro:     dbCEP.Bairro,
		Localidade: dbCEP.Localidade,
		Uf:         dbCEP.Uf,
	}, nil
}

func (r *repository) Create(cepInfo *cep.CEPInfo) error {
	dbCEP := &schema.CEP{
		Cep:        cepInfo.Cep,
		Logradouro: cepInfo.Logradouro,
		Complement: cepInfo.Complement,
		Bairro:     cepInfo.Bairro,
		Localidade: cepInfo.Localidade,
		Uf:         cepInfo.Uf,
	}
	return r.db.Create(dbCEP).Error
}
