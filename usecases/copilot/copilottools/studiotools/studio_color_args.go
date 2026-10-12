package studiotools

type studioCurvePointArgs struct {
	X float64 `json:"x" req:"true" desc:"tom de entrada, de 0 (preto) a 1 (branco)"`
	Y float64 `json:"y" req:"true" desc:"tom de saída, de 0 a 1; acima da diagonal clareia, abaixo escurece"`
}

type studioCurvesArgs struct {
	Master []studioCurvePointArgs `json:"master,omitempty" desc:"curva de luz nos três canais juntos; pelo menos 2 pontos com x crescente"`
	Red    []studioCurvePointArgs `json:"red,omitempty" desc:"curva só do vermelho, aplicada depois de master"`
	Green  []studioCurvePointArgs `json:"green,omitempty" desc:"curva só do verde, aplicada depois de master"`
	Blue   []studioCurvePointArgs `json:"blue,omitempty" desc:"curva só do azul, aplicada depois de master"`
}

type studioHSLArgs struct {
	Color      string   `json:"color,omitempty" enum:"red,orange,yellow,green,cyan,blue,purple,magenta" desc:"atalho para a matiz escolhida (vence hue)"`
	Hue        *float64 `json:"hue,omitempty" desc:"matiz escolhida em graus, de 0 a 360 (0 vermelho, 120 verde, 225 azul)"`
	Width      *float64 `json:"width,omitempty" desc:"largura da faixa de matiz escolhida, de 0 a 360 graus (padrão 40)"`
	Softness   *float64 `json:"softness,omitempty" desc:"borda suave da faixa, de 0 a 180 graus (padrão 30)"`
	Desaturate *float64 `json:"desaturate,omitempty" desc:"quanto tirar a cor de tudo fora da faixa, de 0 a 100; 100 deixa só a cor escolhida"`
	Shift      *float64 `json:"shift,omitempty" desc:"gira a matiz da faixa escolhida, de -180 a 180 graus (troca uma cor por outra)"`
	Saturation *float64 `json:"saturation,omitempty" desc:"saturação da faixa escolhida, de -100 a 100"`
}

type studioClipColorArgs struct {
	GradientMap *studioGradientMapArgs `json:"gradient_map,omitempty" desc:"set_color: troca os tons do clipe de vídeo ou imagem pelas cores do mapa (duotom); campos omitidos mantêm o mapa atual; clear_color gradient_map tira"`
}

type studioSpeedPointArgs struct {
	At    float64 `json:"at" req:"true" desc:"lugar no clipe, de 0 (início) a 1 (fim); o primeiro ponto é 0 e o último 1"`
	Speed float64 `json:"speed" req:"true" desc:"velocidade nesse ponto, de 0.1 a 16"`
}

type studioSpeedCurveArgs struct {
	RampMode         string                 `json:"ramp_mode,omitempty" enum:"smooth,steps" desc:"speed_ramp: smooth (padrão) muda a velocidade de forma contínua por uma curva no mesmo clipe; steps divide o clipe em degraus de velocidade constante"`
	SpeedPoints      []studioSpeedPointArgs `json:"speed_points,omitempty" desc:"set_speed_curve: pontos da curva de velocidade ao longo do clipe, de at 0 a at 1, pelo menos 0.02 entre dois pontos"`
	SpeedCurvePreset string                 `json:"speed_curve_preset,omitempty" enum:"montage,hero,bullet,jumpCut,flashIn,flashOut" desc:"set_speed_curve: curva pronta; montage acelera e freia duas vezes, hero e bullet freiam no meio (câmera lenta de impacto), jumpCut dá um salto rápido no meio, flashIn entra rápido, flashOut sai rápido"`
	ClearSpeedCurve  *bool                  `json:"clear_speed_curve,omitempty" desc:"set_speed_curve: true tira a curva e deixa uma velocidade constante que mantém a mesma mídia e a mesma duração"`
	FrameMix         *bool                  `json:"frame_mix,omitempty" desc:"set_speed: true mistura os dois quadros vizinhos da mídia na câmera lenta para o movimento não travar (só vídeo)"`
}

const studioColorGuide = "Cor além dos ajustes: set_color também aceita curves (master, red, green, blue: pontos {x, y} de 0 a 1, x crescente; master vem antes dos canais), curves_preset (contrast, soften, fade, brighten, darken: troca a curva master), " +
	"hsl (cor seletiva: color ou hue escolhe a matiz, width e softness a faixa; desaturate 100 deixa só essa cor e tira o resto, shift troca a cor dela, saturation reforça ou apaga só ela), lut_media_id (um LUT .cube 3D da biblioteca, aplicado depois dos ajustes) com lut_intensity de 0 a 1, " +
	"e clear_color (curves, hsl, lut, gradient_map) para tirar partes. Ordem da imagem: ajustes de luz e cor, LUT, curvas, cor seletiva, mapa de gradiente. look troca só os ajustes de luz e cor e mantém curvas, cor seletiva e LUT; reset_color tira tudo. "

const studioVideoColorGuide = "Em clipes de vídeo e imagem, set_color aceita gradient_map (preset ou stops, intensity, reverse, interpolation) como nas imagens do editor de imagem. " +
	"A correção anima no tempo com animate property exposure, contrast, highlights, shadows, whites, blacks, temperature, tint, saturation ou vibrance (chaves com value nas faixas de color_grade), em clipes de vídeo e imagem. "

const studioSpeedCurveGuide = "Curva de velocidade (rampa suave, sem cortar o clipe): set_speed_curve(clip_id de vídeo ou áudio, speed_curve_preset ou speed_points, smooth? padrão true curva suave pelos pontos, keep_duration? padrão false mantém a mesma mídia e muda a duração, ripple?, clear_speed_curve?): o som ligado segue a mesma curva; " +
	"speed_ramp com ramp_mode smooth (padrão) muda a velocidade de forma contínua a partir de at_ms e mantém a nova velocidade até o fim. animate property speed edita as chaves de velocidade direto e mantém a duração (a mídia acompanha); chaves vazias voltam para 1. " +
	"set_speed com speed troca a curva por uma velocidade constante; frame_mix true suaviza a câmera lenta misturando quadros. studio_read mostra speed_curve com os pontos (at e speed), smooth e source_ms. "
