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
	Posicion     int    `json:"posicion"`
	Caracter     string `json:"caracter"`
	Identificador string `json:"identificador"`
	Detalle      string `json:"detalle"`
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

// ----------- IMPORTANTE: limpiar apellido para casos como "DE LEÓN", "DEL RÍO", etc. -----------
// Queremos que: "DE LEÓN" -> "LEON" (para que la inicial sea L, no D)
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

	// Si todo era partícula (raro), regresamos el original normalizado
	if len(out) == 0 {
		return apellido
	}

	return strings.Join(out, " ")
}

// Normaliza: mayúsculas, quita acentos, conserva Ñ, y deja solo letras/espacios
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

func validateGen(req GenReq) (GenReq, time.Time, string) {
	req.Nombre = normalizeName(req.Nombre)

	// Aquí aplicamos limpiarApellido para que "DE LEÓN" no tome la D
	req.Paterno = limpiarApellido(req.Paterno)
	req.Materno = limpiarApellido(req.Materno)

	req.Sexo = strings.ToUpper(strings.TrimSpace(req.Sexo))
	req.Estado = strings.ToUpper(strings.TrimSpace(req.Estado))

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

	return req, t, ""
}

// CURP (18): 4 + 6 + 1 + 2 + 3 + 2
// Nota: homoclave simplificada "00" (para cumplir estructura).
func generarCURP(req GenReq, fecha time.Time) string {
	nom := pickNombre(req.Nombre)

	p1 := firstLetter(req.Paterno)
	v1 := firstInternalVowel(req.Paterno)
	m1 := firstLetter(req.Materno) // aquí ya no será D si el materno fue "DE LEÓN"
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

var page = template.Must(template.New("p").Parse(`
<!doctype html>
<html lang="es">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Analizador Sintáctico y Léxico CURP (Go)</title>
<style>
body{font-family:Arial;margin:20px;max-width:1200px}
.grid{display:grid;grid-template-columns:1fr 1fr;gap:18px}
.card{border:1px solid #ddd;border-radius:12px;padding:16px}
.row{display:grid;grid-template-columns:1fr 1fr;gap:12px}
label{font-weight:700;display:block;margin-bottom:6px}
input,select{width:100%;padding:10px;border:1px solid #bbb;border-radius:8px;font-size:15px}
button{margin-top:12px;padding:10px 14px;border:0;border-radius:8px;cursor:pointer}
table{border-collapse:collapse;width:100%;margin-top:12px}
th,td{border:1px solid #999;padding:10px;text-align:left;vertical-align:top}
th{background:#f2f2f2}
.ok{color:#0a7a2f;font-weight:800}
.err{color:#b00020;font-weight:800}
code{font-size:16px}
small{color:#444}
h2{margin-top:0}
</style>
</head>
<body>
<h1>CURP: Analizador Sintáctico (genera) + Analizador Léxico (explica)</h1>
<p><small>Estados permitidos por tu profe: DG (Durango) y JC (Jalisco)</small></p>
<p><small>Apellidos con partículas: DE, DEL, LA, LAS, LOS, MC, MAC, VAN, VON se ignoran para tomar la inicial (ej: "DE LEÓN" -> "LEON").</small></p>

<div class="grid">
  <div class="card">
    <h2>1) Analizador Sintáctico (Generar CURP)</h2>

    <div class="row">
      <div>
        <label>Apellido paterno</label>
        <input id="paterno" placeholder="Ej: VELASCO">
      </div>
      <div>
        <label>Apellido materno</label>
        <input id="materno" placeholder="Ej: DE LEÓN">
      </div>
      <div>
        <label>Nombre(s)</label>
        <input id="nombre" placeholder="Ej: ANDERSON">
      </div>
      <div>
        <label>Fecha de nacimiento</label>
        <input id="fecha" type="date">
      </div>
      <div>
        <label>Sexo</label>
        <select id="sexo">
          <option value="H">H</option>
          <option value="M">M</option>
        </select>
      </div>
      <div>
        <label>Estado</label>
        <select id="estado">
          <option value="DG">DG - Durango</option>
          <option value="JC">JC - Jalisco</option>
        </select>
      </div>
    </div>

    <button id="btnGen">Generar CURP</button>
    <div id="msgGen" style="margin-top:12px"></div>
    <div id="curpBox" style="margin-top:10px"></div>
  </div>

  <div class="card">
    <h2>2) Analizador Léxico (Tokens de la CURP)</h2>
    <div id="msgLex"></div>
    <div id="lexOut"></div>
  </div>
</div>

<script>
async function generar(){
  const payload = {
    paterno: document.getElementById("paterno").value,
    materno: document.getElementById("materno").value,
    nombre: document.getElementById("nombre").value,
    fecha: document.getElementById("fecha").value,
    sexo: document.getElementById("sexo").value,
    estado: document.getElementById("estado").value
  };

  const msgGen = document.getElementById("msgGen");
  const curpBox = document.getElementById("curpBox");
  const msgLex = document.getElementById("msgLex");
  const lexOut = document.getElementById("lexOut");

  msgGen.innerHTML = "";
  curpBox.innerHTML = "";
  msgLex.innerHTML = "";
  lexOut.innerHTML = "";

  const res = await fetch("/api/generar", {
    method:"POST",
    headers:{"Content-Type":"application/json"},
    body: JSON.stringify(payload)
  });

  const data = await res.json();
  if(!data.ok){
    msgGen.innerHTML = '<div class="err">' + data.error + '</div>';
    return;
  }

  msgGen.innerHTML = '<div class="ok">CURP generada correctamente</div>';
  curpBox.innerHTML = '<p><b>CURP:</b> <code>' + data.curp + '</code></p>';

  await lexical(data.curp);
}

async function lexical(curp){
  const msgLex = document.getElementById("msgLex");
  const lexOut = document.getElementById("lexOut");
  msgLex.innerHTML = "";
  lexOut.innerHTML = "";

  const res = await fetch("/api/lexico", {
    method:"POST",
    headers:{"Content-Type":"application/json"},
    body: JSON.stringify({curp})
  });

  const data = await res.json();
  if(!data.ok){
    msgLex.innerHTML = '<div class="err">' + data.error + '</div>';
    return;
  }

  msgLex.innerHTML = '<div class="ok">Tokens identificados</div>';

  let html = '<table><tr><th>Posición</th><th>Carácter</th><th>Identificador</th><th>Detalle</th></tr>';
  for(const t of data.tokens){
    html += '<tr><td>' + t.posicion + '</td><td><code>' + t.caracter + '</code></td><td>' + t.identificador + '</td><td>' + t.detalle + '</td></tr>';
  }
  html += '</table>';
  lexOut.innerHTML = html;
}

document.getElementById("btnGen").addEventListener("click", generar);
</script>
</body>
</html>
`))

func main() {
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