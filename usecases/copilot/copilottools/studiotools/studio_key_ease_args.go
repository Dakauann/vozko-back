package studiotools

import (
	"fmt"
	"reflect"
	"strings"

	"vozko/usecases/copilot/copilottools"
)

const (
	minKeyInfluence = 0.1
	maxKeyInfluence = 100.0
)

type studioKeyEaseArgs struct {
	KeysMS    []int64  `json:"keys_ms,omitempty" desc:"ease_keys, smooth_keys, reverse_keys, time_stretch_keys e rove_keys: instantes das chaves em ms contados do início do clipe, como em studio_read full; vazio usa todas as chaves de property, ou de todas as propriedades animadas sem property"`
	KeySpeed  *float64 `json:"key_speed,omitempty" desc:"ease_keys: velocidade naquele lado da chave por segundo, na unidade dos valores (x e y em fração do quadro, scale em vezes, rotation em graus, opacity de 0 a 1, position em alturas do quadro por segundo pelo caminho); 0 para na chave; fill e stroke só aceitam 0"`
	Influence *float64 `json:"influence,omitempty" desc:"ease_keys: influência em por cento, de 0.1 a 100: quanto do trecho a curva usa para chegar na velocidade; 33.3 é a suavização padrão, valores altos seguram mais"`
	Smooth    *bool    `json:"smooth,omitempty" desc:"smooth_keys: true (padrão) deixa a velocidade contínua automática nas chaves; false volta para as curvas manuais"`
	Percent   *float64 `json:"percent,omitempty" desc:"time_stretch_keys: o novo tempo entre as chaves em por cento; 200 dobra, 50 corta pela metade"`
	FromKey   string   `json:"from_key,omitempty" enum:"first,last" desc:"time_stretch_keys: a chave que fica parada; first (padrão) estica para a frente, last para trás"`
}

func (a studioKeyEaseArgs) argsIssue() string {
	switch {
	case a.Influence != nil && (*a.Influence < minKeyInfluence || *a.Influence > maxKeyInfluence):
		return fmt.Sprintf("influence vai de %g a %g (por cento)", minKeyInfluence, maxKeyInfluence)
	case a.Percent != nil && *a.Percent <= 0:
		return "percent precisa ser maior que 0"
	}
	for _, at := range a.KeysMS {
		if at < 0 {
			return "keys_ms conta do início do clipe e não aceita instantes negativos"
		}
	}
	return ""
}

type argsChecker interface {
	argsIssue() string
}

func operationIssue(op reflect.Value) string {
	for _, f := range reflect.VisibleFields(op.Type()) {
		field := op.FieldByIndex(f.Index)
		if allowed := copilottools.EnumValues(f); allowed != nil && field.Kind() == reflect.String && field.String() != "" && !copilottools.Contains(allowed, field.String()) {
			return fmt.Sprintf("%s deve ser um de: %s", copilottools.JSONName(f), strings.Join(allowed, ", "))
		}
	}
	if checker, ok := op.Interface().(argsChecker); ok {
		return checker.argsIssue()
	}
	return ""
}
