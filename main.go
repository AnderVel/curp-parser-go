package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
)

type GenReq struct {
	Nombre  string `json:"nombre"`
	Paterno string `json:"paterno"`
	Materno string `json:"materno"`
	Fecha   string `json:"fecha"`  // YYYY-MM-DD
	Sexo    string `json:"sexo"`   // H o M
	Estado  string `json:"estado"` // DG o JC
}

type GenResp struct {
	OK    bool   `json:"ok"`
	CURP  string `json:"curp,omitempty"`
	Error string `json:"error,omitempty"`
}

type LexRow struct {
	Posicion      int    `json:"posicion"`
	Caracter      string `json:"caracter"`
	Identificador string `json:"identificador"`
	Detalle       string `json:"detalle"`
}

type LexResp struct {
	OK     bool     `json:"ok"`
	CURP   string   `json:"curp,omitempty"`
	Tokens []LexRow `json:"tokens,omitempty"`
	Error  string   `json:"error,omitempty"`
}

var estadosPermitidos = map[string]string{
	"DG": "Durango",
	"JC": "Jalisco",
}

var nombresIgnorar = map[string]bool{
	"JOSE":  true,
	"MARIA": true,
	"MA":    true,
	"J":     true,
}

func limpiarApellido(apellido string) string {
	particulas := map[string]bool{
		"DE":  true,
		"DEL": true,
		"LA":  true,
		"LAS": true,
		"LOS": true,
		"MC":  true,
		"MAC": true,
		"VAN": true,
		"VON": true,
	}

	apellido = normalizeName(apellido)
	palabras := strings.Fields(apellido)

	var out []string
	for _, palabra := range palabras {
		if !particulas[palabra] {
			out = append(out, palabra)
		}
	}

	if len(out) == 0 {
		return apellido
	}

	return strings.Join(out, " ")
}

func normalizeName(s string) string {
	s = strings.TrimSpace(strings.ToUpper(s))
	repl := strings.NewReplacer(
		"Á", "A", "É", "E", "Í", "I", "Ó", "O", "Ú", "U",
		"Ü", "U",
	)
	s = repl.Replace(s)

	var b strings.Builder
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || r == 'Ñ' || r == ' ' {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func pickNombre(nombre string) string {
	parts := strings.Fields(nombre)
	if len(parts) == 0 {
		return ""
	}
	if len(parts) >= 2 && nombresIgnorar[parts[0]] {
		return parts[1]
	}
	return parts[0]
}

func isVowel(r rune) bool {
	switch r {
	case 'A', 'E', 'I', 'O', 'U':
		return true
	}
	return false
}

func firstLetter(s string) rune {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return r
		}
	}
	return 'X'
}

func firstInternalVowel(s string) rune {
	rs := []rune(s)
	for i := 1; i < len(rs); i++ {
		if isVowel(rs[i]) {
			return rs[i]
		}
	}
	return 'X'
}

func firstInternalConsonant(s string) rune {
	rs := []rune(s)
	for i := 1; i < len(rs); i++ {
		if unicode.IsLetter(rs[i]) && !isVowel(rs[i]) {
			return rs[i]
		}
	}
	return 'X'
}

// --- NUEVO: valida que la fecha sea REAL (incluye bisiestos) ---
// Ej: 2001-02-29 -> inválida, 2000-02-29 -> válida
func isValidDateStrict(fechaStr string, t time.Time) bool {
	return t.Format("2006-01-02") == fechaStr
}

// --- NUEVO: Validaciones de NOMBRE/APELLIDOS ---
// 1) No aceptar números ni caracteres (.,-,_,etc). Solo letras y espacios.
// 2) Mínimo 3 letras (después de limpiar/normalizar).
// 3) No aceptar "AAA" ni cadenas de 3+ letras todas iguales (ej: "AAAA", "BBBBB").
func onlyLettersAndSpacesOriginal(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for _, r := range s {
		if r == ' ' {
			continue
		}
		if unicode.IsLetter(r) {
			continue
		}
		// si es dígito, puntuación, símbolo, etc.
		return false
	}
	return true
}

func lettersCount(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			n++
		}
	}
	return n
}

func allLettersSame(s string) bool {
	var first rune
	set := false
	for _, r := range s {
		if !unicode.IsLetter(r) {
			continue
		}
		if !set {
			first = r
			set = true
			continue
		}
		if r != first {
			return false
		}
	}
	return set // true si hubo letras y todas eran iguales
}

func validateNameFieldOriginal(label, raw string) string {
	if strings.TrimSpace(raw) == "" {
		return fmt.Sprintf("Faltan datos: %s es obligatorio.", label)
	}
	if !onlyLettersAndSpacesOriginal(raw) {
		return fmt.Sprintf("%s inválido: no se permiten números ni caracteres especiales (solo letras y espacios).", label)
	}
	return ""
}

func validateNameFieldNormalized(label, normalized string) string {
	if lettersCount(normalized) < 3 {
		return fmt.Sprintf("%s inválido: debe tener mínimo 3 letras.", label)
	}
	// "AAA" y similares (3+ letras iguales) no permitidos
	if allLettersSame(normalized) && lettersCount(normalized) >= 3 {
		return fmt.Sprintf("%s inválido: no se permite usar 'AAA' (o letras repetidas).", label)
	}
	return ""
}

func validateGen(req GenReq) (GenReq, time.Time, string) {
	// --- NUEVO: validar RAW antes de normalizar (para detectar 1ANDERSON, .ANDERSON, etc.) ---
	if msg := validateNameFieldOriginal("Nombre", req.Nombre); msg != "" {
		return req, time.Time{}, msg
	}
	if msg := validateNameFieldOriginal("Apellido paterno", req.Paterno); msg != "" {
		return req, time.Time{}, msg
	}
	if msg := validateNameFieldOriginal("Apellido materno", req.Materno); msg != "" {
		return req, time.Time{}, msg
	}

	req.Nombre = normalizeName(req.Nombre)
	req.Paterno = limpiarApellido(req.Paterno)
	req.Materno = limpiarApellido(req.Materno)
	req.Sexo = strings.ToUpper(strings.TrimSpace(req.Sexo))
	req.Estado = strings.ToUpper(strings.TrimSpace(req.Estado))

	// --- NUEVO: validar ya normalizado/limpio (mínimo 3 letras y no AAA) ---
	if msg := validateNameFieldNormalized("Nombre", req.Nombre); msg != "" {
		return req, time.Time{}, msg
	}
	if msg := validateNameFieldNormalized("Apellido paterno", req.Paterno); msg != "" {
		return req, time.Time{}, msg
	}
	if msg := validateNameFieldNormalized("Apellido materno", req.Materno); msg != "" {
		return req, time.Time{}, msg
	}

	if req.Paterno == "" || req.Materno == "" || req.Nombre == "" {
		return req, time.Time{}, "Faltan datos: Nombre, Apellido paterno y materno son obligatorios."
	}
	if req.Sexo != "H" && req.Sexo != "M" {
		return req, time.Time{}, "Sexo inválido: usa H o M."
	}
	if _, ok := estadosPermitidos[req.Estado]; !ok {
		return req, time.Time{}, "Estado inválido: solo DG (Durango) o JC (Jalisco)."
	}

	matched, _ := regexp.MatchString(`^\d{4}-\d{2}-\d{2}$`, req.Fecha)
	if !matched {
		return req, time.Time{}, "Fecha inválida. Formato: YYYY-MM-DD."
	}
	t, err := time.Parse("2006-01-02", req.Fecha)
	if err != nil {
		return req, time.Time{}, "Fecha inválida."
	}

	// --- NUEVO: valida bisiestos/fecha real ---
	if !isValidDateStrict(req.Fecha, t) {
		return req, time.Time{}, "Fecha inválida (revisa mes/día y años bisiestos)."
	}

	// --- NUEVO: no permitir fechas de nacimiento de hace más de 110 años ---
	hoy := time.Now()
	hoy = time.Date(hoy.Year(), hoy.Month(), hoy.Day(), 0, 0, 0, 0, hoy.Location())

	limite := hoy.AddDate(-110, 0, 0)
	if t.Before(limite) {
		return req, time.Time{}, "Fecha inválida: no se permiten fechas de nacimiento mayores a 110 años."
	}
	if t.After(hoy) {
		return req, time.Time{}, "Fecha inválida: no se permiten fechas futuras."
	}

	return req, t, ""
}

func generarCURP(req GenReq, fecha time.Time) string {
	nom := pickNombre(req.Nombre)

	p1 := firstLetter(req.Paterno)
	v1 := firstInternalVowel(req.Paterno)
	m1 := firstLetter(req.Materno)
	n1 := firstLetter(nom)

	yy := fmt.Sprintf("%02d", fecha.Year()%100)
	mm := fmt.Sprintf("%02d", int(fecha.Month()))
	dd := fmt.Sprintf("%02d", fecha.Day())

	sexo := req.Sexo
	estado := req.Estado

	c1 := firstInternalConsonant(req.Paterno)
	c2 := firstInternalConsonant(req.Materno)
	c3 := firstInternalConsonant(nom)

	h1, h2 := "0", "0"

	return fmt.Sprintf("%c%c%c%c%s%s%s%s%s%c%c%c%s%s",
		p1, v1, m1, n1,
		yy, mm, dd,
		sexo,
		estado,
		c1, c2, c3,
		h1, h2,
	)
}

func lexCURP(curp string) []LexRow {
	r := []rune(curp)

	get := func(i int) string { return string(r[i]) }
	get2 := func(i, j int) string { return string(r[i:j]) }

	estado := get2(11, 13)
	estadoNombre := estadosPermitidos[estado]
	sexo := get(10)

	sexoDet := "Sexo"
	if sexo == "H" {
		sexoDet = "Hombre (H)"
	} else if sexo == "M" {
		sexoDet = "Mujer (M)"
	}

	return []LexRow{
		{1, get(0), "identidad", "Letra inicial del primer apellido"},
		{2, get(1), "identidad", "Primera vocal interna del primer apellido"},
		{3, get(2), "identidad", "Letra inicial del segundo apellido"},
		{4, get(3), "identidad", "Letra inicial del nombre (si es compuesto y empieza con Jose/Maria, se toma el segundo)"},
		{5, get(4), "fecha_nacimiento", "Año (YY) - primer dígito"},
		{6, get(5), "fecha_nacimiento", "Año (YY) - segundo dígito"},
		{7, get(6), "fecha_nacimiento", "Mes (MM) - primer dígito"},
		{8, get(7), "fecha_nacimiento", "Mes (MM) - segundo dígito"},
		{9, get(8), "fecha_nacimiento", "Día (DD) - primer dígito"},
		{10, get(9), "fecha_nacimiento", "Día (DD) - segundo dígito"},
		{11, get(10), "sexo", sexoDet},
		{12, get(11), "estado", fmt.Sprintf("Estado: %s (%s) - 1ra letra", estadoNombre, estado)},
		{13, get(12), "estado", fmt.Sprintf("Estado: %s (%s) - 2da letra", estadoNombre, estado)},
		{14, get(13), "consonante_interna", "Primera consonante interna del primer apellido"},
		{15, get(14), "consonante_interna", "Primera consonante interna del segundo apellido"},
		{16, get(15), "consonante_interna", "Primera consonante interna del nombre"},
		{17, get(16), "homoclave", "Homoclave (simplificada) - carácter 1"},
		{18, get(17), "homoclave", "Homoclave (simplificada) - carácter 2"},
	}
}

func validateCurpChars(curp string) string {
	if len(curp) != 18 {
		return "La CURP generada debe tener 18 caracteres."
	}
	for _, ch := range curp {
		if !(unicode.IsLetter(ch) || unicode.IsDigit(ch)) {
			return "CURP inválida: contiene caracteres no alfanuméricos."
		}
	}
	return ""
}

func main() {
	page := template.Must(template.ParseFiles("templates/index.html"))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_ = page.Execute(w, nil)
	})

	http.HandleFunc("/api/generar", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
			return
		}
		var req GenReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			_ = json.NewEncoder(w).Encode(GenResp{OK: false, Error: "JSON inválido"})
			return
		}

		req, fecha, msg := validateGen(req)
		if msg != "" {
			_ = json.NewEncoder(w).Encode(GenResp{OK: false, Error: msg})
			return
		}

		curp := generarCURP(req, fecha)
		if em := validateCurpChars(curp); em != "" {
			_ = json.NewEncoder(w).Encode(GenResp{OK: false, Error: em})
			return
		}

		_ = json.NewEncoder(w).Encode(GenResp{OK: true, CURP: curp})
	})

	http.HandleFunc("/api/lexico", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			CURP string `json:"curp"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			_ = json.NewEncoder(w).Encode(LexResp{OK: false, Error: "JSON inválido"})
			return
		}

		curp := strings.TrimSpace(strings.ToUpper(req.CURP))
		curp = strings.Join(strings.Fields(curp), "")

		if em := validateCurpChars(curp); em != "" {
			_ = json.NewEncoder(w).Encode(LexResp{OK: false, Error: em})
			return
		}

		estado := curp[11:13]
		if _, ok := estadosPermitidos[estado]; !ok {
			_ = json.NewEncoder(w).Encode(LexResp{OK: false, Error: "Estado no permitido en la CURP. Solo DG o JC."})
			return
		}

		tokens := lexCURP(curp)
		_ = json.NewEncoder(w).Encode(LexResp{OK: true, CURP: curp, Tokens: tokens})
	})

	fmt.Println("Abre: http://localhost:9090")
	_ = http.ListenAndServe(":9090", nil)
}