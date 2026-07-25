package auth

import (
	"strings"

	"vozko/domain/user"
)

func (r *LoginRequest) Validate() map[string]string {
	errs := map[string]string{}
	if r.Email == "" {
		errs["email"] = "required"
	}
	if r.Password == "" {
		errs["password"] = "required"
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

func (r *RegisterRequest) Validate() map[string]string {
	r.Name = strings.TrimSpace(r.Name)
	r.Email = strings.TrimSpace(r.Email)
	r.CustomerType = strings.TrimSpace(r.CustomerType)
	r.CPF = strings.TrimSpace(r.CPF)
	r.CNPJ = strings.TrimSpace(r.CNPJ)
	r.VerificationToken = strings.TrimSpace(r.VerificationToken)

	if r.Name == "" || r.Email == "" || r.Password == "" || r.VerificationToken == "" {
		expected := map[string]string{}
		if r.Name == "" {
			expected["name"] = "required"
		}
		if r.Email == "" {
			expected["email"] = "required"
		}
		if r.Password == "" {
			expected["password"] = "required"
		}
		if r.VerificationToken == "" {
			expected["verificationToken"] = "required"
		}
		return expected
	}

	if len(r.Name) < 2 || len(r.Name) > 120 {
		return map[string]string{"name": "must be between 2 and 120 characters"}
	}

	r.CustomerType = strings.ToLower(r.CustomerType)
	if r.CustomerType == "" {
		return map[string]string{"customerType": "required"}
	}

	switch r.CustomerType {
	case string(user.CustomerTypeIndividual):
		if r.CPF == "" {
			return map[string]string{"cpf": "required when customerType='individual'"}
		}
	case string(user.CustomerTypeCompany):
		if r.CNPJ == "" {
			return map[string]string{"cnpj": "required when customerType='company'"}
		}
	default:
		return map[string]string{"customerType": "must be 'individual' or 'company'"}
	}

	return nil
}

func (r *AdminRegisterRequest) Validate() map[string]string {
	r.Name = strings.TrimSpace(r.Name)
	r.Email = strings.TrimSpace(r.Email)
	r.CustomerType = strings.TrimSpace(r.CustomerType)
	r.CPF = strings.TrimSpace(r.CPF)
	r.CNPJ = strings.TrimSpace(r.CNPJ)

	if r.Name == "" || r.Email == "" || r.Password == "" {
		expected := map[string]string{}
		if r.Name == "" {
			expected["name"] = "required"
		}
		if r.Email == "" {
			expected["email"] = "required"
		}
		if r.Password == "" {
			expected["password"] = "required"
		}
		return expected
	}

	if len(r.Name) < 2 || len(r.Name) > 120 {
		return map[string]string{"name": "must be between 2 and 120 characters"}
	}

	r.CustomerType = strings.ToLower(r.CustomerType)
	if r.CustomerType == "" {
		return map[string]string{"customerType": "required"}
	}

	switch r.CustomerType {
	case string(user.CustomerTypeIndividual):
		if r.CPF == "" {
			return map[string]string{"cpf": "required when customerType='individual'"}
		}
	case string(user.CustomerTypeCompany):
		if r.CNPJ == "" {
			return map[string]string{"cnpj": "required when customerType='company'"}
		}
	default:
		return map[string]string{"customerType": "must be 'individual' or 'company'"}
	}

	return nil
}

func (r *ForgotPasswordRequest) Validate() map[string]string {
	r.Email = strings.TrimSpace(r.Email)
	if r.Email == "" {
		return map[string]string{"email": "required"}
	}
	return nil
}

func (r *ResetPasswordRequest) Validate() map[string]string {
	r.Email = strings.TrimSpace(r.Email)
	r.Token = strings.TrimSpace(r.Token)
	r.NewPassword = strings.TrimSpace(r.NewPassword)

	if r.Email == "" || r.Token == "" || r.NewPassword == "" {
		expected := map[string]string{}
		if r.Email == "" {
			expected["email"] = "required"
		}
		if r.Token == "" {
			expected["token"] = "required"
		}
		if r.NewPassword == "" {
			expected["newPassword"] = "required"
		}
		return expected
	}
	return nil
}

func (r *SendEmailVerificationRequest) Validate() map[string]string {
	r.Email = strings.TrimSpace(r.Email)
	if r.Email == "" {
		return map[string]string{"email": "required"}
	}
	return nil
}

func (r *VerifyEmailRequest) Validate() map[string]string {
	r.Token = strings.TrimSpace(r.Token)
	if r.Token == "" {
		return map[string]string{"token": "required"}
	}
	return nil
}

func (r *ChangePasswordRequest) Validate() map[string]string {
	errs := map[string]string{}
	if r.CurrentPassword == "" {
		errs["currentPassword"] = "required"
	}
	if r.NewPassword == "" {
		errs["newPassword"] = "required"
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}
