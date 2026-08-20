# Publicar TaskKeeper en macOS — guía completa (lo que queda del §27.5)

Estado: el código de macOS está escrito y **cross-compila** (verificado `darwin-x64` y `darwin-arm64`), y hay un **compile-check en cada release** para que no se rompa. Lo que falta para poder ofrecerlo a usuarios de Mac depende de **ti** (certificados + un Mac + una beta), no del agente. Este documento es la lista de tareas.

> Regla de oro: en macOS, un binario **sin firmar y sin notarizar** lo **bloquea Gatekeeper** en el primer arranque («no se puede abrir porque Apple no puede comprobar si contiene malware»). Por eso firmar+notarizar es **obligatorio** para Mac, no opcional.

---

## 0. Resumen de costes y de qué depende cada cosa

| Cosa | ¿Obligatoria? | Coste aprox. | La saca / la hace |
|---|---|---|---|
| **Apple Developer Program** | **Sí** (para Mac) | **99 USD / año** (~92 €) | Tú (a tu nombre o el de Argalla) |
| **Certificado «Developer ID Application»** | Sí (para Mac) | Incluido en la membresía | Tú, desde el portal de Apple |
| **Firma + notarización** de los binarios darwin | Sí (para Mac) | 0 € (herramientas gratis) | Requiere **macOS**: un Mac real o el runner `macos-latest` gratis de GitHub Actions |
| **Verificar en un Mac real** | Sí (una vez) | 0 € (cualquier Mac, una tarde) | Tú (o quien tenga un Mac) |
| **Firma de Windows (Authenticode)** | **Opcional** (ya funciona sin ella) | ~120 USD/año (Azure) o 200-400 €/año (certificado) | Tú |
| **Beta corta** antes de quitar «preview» | Recomendada | 0 € | Tú + 3-4 usuarios |

**Coste mínimo real para llegar a Mac: 99 USD/año de Apple.** Lo demás es tiempo (y opcionalmente firma de Windows).

Las cifras son aproximadas (2026) y pueden cambiar; confírmalas en las páginas oficiales antes de pagar.

---

## Parte A — Apple (OBLIGATORIO para Mac)

### A1. Alta en el Apple Developer Program (99 USD/año)
1. Ve a https://developer.apple.com/programs/ e inscríbete con un Apple ID.
2. Puedes inscribirte como **individuo** (aparece tu nombre) o como **organización** (aparece «Argalla»; requiere un D-U-N-S Number gratuito de tu empresa y tarda unos días en verificarse). Para un producto de marca, mejor **organización**.
3. Pago 99 USD/año. Renovación anual: si caduca, los binarios ya notarizados siguen funcionando, pero no podrás notarizar nuevas versiones.

### A2. Crear el certificado «Developer ID Application»
Este es el certificado con el que se **firman** binarios que se distribuyen **fuera** de la Mac App Store (nuestro caso: se instalan por el VSIX de la extensión).
1. En https://developer.apple.com/account → **Certificates, IDs & Profiles** → **Certificates** → «+».
2. Elige **Developer ID Application** (NO «Mac App Distribution»; ese es solo para la App Store).
3. Sigue el asistente (te pedirá un CSR que generas con Acceso a Llaveros en un Mac, o con `openssl`).
4. Descarga el `.cer`, instálalo en el llavero de un Mac, y **expórtalo como `.p12`** (con contraseña). Ese `.p12` + su contraseña son lo que usará el CI para firmar. Guárdalos como secretos (nunca en el repo).

### A3. Credenciales para notarizar (API Key)
La notarización necesita autenticarse contra el servicio de Apple. Lo moderno es una **App Store Connect API Key** (evita meter tu Apple ID/contraseña en el CI):
1. https://appstoreconnect.apple.com/access/api → crea una **API Key** con rol «Developer».
2. Te da: un **Key ID**, un **Issuer ID** y un fichero `.p8` (¡se descarga UNA sola vez!). Esos tres son secretos para el CI.

### A4. Qué se firma y cómo (per-binario, son CLI, no `.app`)
Nuestros binarios son ejecutables de línea de órdenes (`taskkeeper-worker`, `taskkeeper-ctl`), no un paquete `.app`. Se firman uno a uno, con **hardened runtime**:

```bash
# En un Mac (o runner macOS), tras importar el .p12 al llavero:
codesign --force --options runtime --timestamp \
  --sign "Developer ID Application: TU NOMBRE (TEAMID)" \
  bin/darwin-arm64/taskkeeper-worker
codesign --force --options runtime --timestamp \
  --sign "Developer ID Application: TU NOMBRE (TEAMID)" \
  bin/darwin-arm64/taskkeeper-ctl
# (repetir para darwin-x64)
```

### A5. Notarizar y «grapar» (staple)
Notarizar = subir los binarios a Apple, que los escanea y devuelve un «ticket» de aprobación. Luego se «grapa» el ticket al binario para que funcione sin conexión:

```bash
# Se empaquetan los binarios firmados en un zip y se envían:
ditto -c -k --keepParent bin/darwin-arm64 darwin-arm64.zip
xcrun notarytool submit darwin-arm64.zip \
  --key AuthKey_XXXX.p8 --key-id KEY_ID --issuer ISSUER_ID --wait
# Cuando dice "Accepted", grapar el ticket (para binarios sueltos se grapa el zip
# de distribución; para CLI sueltos suele bastar con que estén notarizados —
# verifícalo con `spctl -a -vv -t install <binario>`).
```

> **Importante:** todo esto (`codesign`, `notarytool`, `spctl`) **solo existe en macOS**. Por eso el trabajo de firma/notarización tiene que correr en un Mac o en un runner `macos-latest` de GitHub Actions (gratis para repos públicos).

---

## Parte B — Windows (OPCIONAL, recomendado)

TaskKeeper **ya funciona hoy en Windows sin firmar** (el `.exe` va dentro del VSIX, no se «descarga» por navegador, así que SmartScreen no suele saltar). Firmar lo pule: evita cualquier aviso de SmartScreen/antivirus y da confianza.

Como desde 2023 los certificados de firma de código deben vivir en hardware o en un HSM en la nube (ya no se puede meter un `.pfx` suelto en el CI), la opción moderna y más barata para CI es:

- **Azure Trusted Signing** (Microsoft): ~9,99 USD/mes. Firma en la nube desde el CI con una acción oficial de GitHub. Requiere una organización con cierta antigüedad (o la opción para individuos donde esté disponible). **Recomendado.**
- Alternativa: certificado **OV/EV code signing** de una CA (Sectigo, DigiCert, SSL.com, Certum): 200-600 €/año, con token USB o servicio en la nube (p. ej. SSL.com eSigner). El **EV** da reputación SmartScreen instantánea; el OV la va ganando con las descargas.

Se firmarían `taskkeeper-worker.exe` y `taskkeeper-ctl.exe` con `signtool` (o la acción de Azure) antes de empaquetar el VSIX de Windows.

---

## Parte C — Cambios en el CI (`.github/workflows/release.yml`)

Hoy el release es **un solo job en `ubuntu-latest`** que compila y publica solo `win32-x64`. Para macOS hay que reestructurar (ya está anotado en `docs/PLAN.md §27.5`):

1. **Tres jobs** con `needs:` entre ellos:
   - `verificar` (una vez): verify-tag, `go test ./...`, `npm run check`, compile-check darwin.
   - `publicar` (matriz, `fail-fast: false`): un target por fila → `{ win32-x64 (ubuntu), darwin-x64 (macos), darwin-arm64 (macos) }`. Cada fila: `build-bin.mjs <target>`, **firmar** (Windows: Azure; macOS: codesign+notarytool), `vsce package/publish --target <target>`, `ovsx publish --target <target>`. Las filas darwin corren en `runs-on: macos-latest`.
   - `release` (una vez): `gh release create` con los 3 VSIX adjuntos.
2. **Idempotencia por `(versión, target)`, no por versión.** ⚠️ La actual salta si «la versión ya existe» — en una matriz con la misma versión, en cuanto `win32-x64` publique, las filas darwin se saltarían y **darwin no se publicaría nunca**. Hay que comprobar por target (vsce/ovsx exponen el target del paquete) o quitar el skip y confiar solo en «already exists / already published».
3. **Secretos nuevos en GitHub** (Settings → Secrets and variables → Actions):
   - macOS: `APPLE_CERT_P12_BASE64` (el `.p12` en base64), `APPLE_CERT_PASSWORD`, `APPLE_API_KEY_P8` (el `.p8`), `APPLE_API_KEY_ID`, `APPLE_API_ISSUER_ID`, `APPLE_TEAM_ID`.
   - Windows (si firmas): los de Azure Trusted Signing (endpoint, account, cert profile) o los de tu servicio de firma.
   - Ya existentes: `VSCE_PAT`, `OVSX_PAT`.

---

## Parte D — Verificar en un Mac real (una vez)

El runner de CI compila y notariza, pero **no prueba el comportamiento**. Hace falta enchufarlo en un Mac (cualquiera, una tarde) y comprobar:
- Que el disparador se registra: TaskKeeper usa **launchd** (un `.plist` en `~/Library/LaunchAgents`). Verifica con `launchctl list | grep taskkeeper`.
- Que **despierta** o al menos ejecuta al encender, y que respeta el cupo y la cancelación (matar el árbol de procesos con el grupo de procesos de macOS).
- Que el binario notarizado abre sin el aviso de Gatekeeper: `spctl -a -vv -t install <binario>` debe decir «accepted».

---

## Parte E — Quitar «preview» (checklist final, tras la beta)

Cuando esté firmado, notarizado, verificado en Mac y con una beta corta hecha, se pasa a GA quitando la etiqueta de vista previa en **tres sitios**:
1. `apps/vscode-extension/package.json` → borrar `"preview": true` (línea ~31).
2. `apps/vscode-extension/README.md` (línea ~40) → cambiar «Windows 10/11, x64 in this release. macOS support is built and unverified…» por el texto de GA con macOS incluido.
3. `.github/workflows/release.yml` → la nota de la GitHub release está hardcodeada como «Windows x64 preview»; actualizarla (y ya no es solo Windows).

---

## Orden recomendado (paso a paso)

1. **Alta en Apple Developer** (99 USD/año) → crear **Developer ID Application** cert (`.p12`) + **API Key** (`.p8` + IDs). *(Parte A)*
2. (Opcional) Montar **Azure Trusted Signing** para Windows. *(Parte B)*
3. Añadir los **secretos** al repo. *(Parte C.3)*
4. **Reestructurar `release.yml`** en 3 jobs + matriz + firma/notarización + idempotencia por (versión,target). *(Parte C)*
5. Tag de prueba (p. ej. `v0.9.0-rc1` en una rama) para ver que el CI firma, notariza y publica los 3 targets sin romper win32. Repetir hasta verde.
6. **Verificar en un Mac real.** *(Parte D)*
7. **Beta** con 3-4 usuarios unos días.
8. **Quitar «preview»** en los 3 sitios y publicar la versión GA. *(Parte E)*

---

## Enlaces oficiales
- Apple Developer Program: https://developer.apple.com/programs/
- Developer ID + notarización: https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution
- `notarytool`: https://developer.apple.com/documentation/technotes/tn3147-migrating-to-the-latest-notarization-tool
- Azure Trusted Signing: https://learn.microsoft.com/azure/trusted-signing/
- VS Code — extensiones por plataforma: https://code.visualstudio.com/api/working-with-extensions/publishing-extension#platformspecific-extensions
