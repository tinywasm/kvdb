# PLAN: kvdb — debounce automático de escritura a disco

## Estado: PENDIENTE DE REVISIÓN

## Contexto

`kvdb` (TinyDB) es un almacén clave-valor respaldado por un archivo `.env`.
Interfaz pública: `New()`, `Get(key)`, `Set(key, value)`. La persistencia se
delega a un `Store` (`GetFile`, `SetFile`, `AddToFile`). En la app real el
`Store` es `FileStore` que escribe sobre disco con `os.WriteFile`.

Esta investigación surgió de la sospecha de que `kvdb` perdía valores cuando un
manejador hace dos `Set()` casi simultáneos (ej. guardar tamaño y posición del
navegador a la vez), por no esperar antes de escribir.

## Hallazgo principal: NO hay pérdida de datos

`Set()` llama a `persist()` (reescritura completa del archivo) o `append()`
**de inmediato** en cada llamada. No hay batching ni espera.

```
Set("browser_size", "900,700")    → os.WriteFile (reescritura #1)
Set("browser_position", "0,0")    → os.WriteFile (reescritura #2)
```

Un `sync.RWMutex` serializa todas las llamadas: el segundo `Set()` solo corre
cuando el primer `os.WriteFile` terminó. **El estado en memoria y en disco
siempre queda consistente.** Ningún valor se pierde por escrituras concurrentes.

### Veredicto

**El debounce NO es la causa de que `browser_position` no se actualice.** Ese
bug está en `devbrowser` (el flag `--window-position` es ignorado por el
compositor; ver `devbrowser/docs/PLAN.md`). Dos `Set()` consecutivos en kvdb son
seguros y correctos.

Test que lo corrobora:
[write_count_test.go](../write_count_test.go) → `TestRapidSetsPreserveAllValues`
verifica que tras 3 `Set()` rápidos todos los valores quedan correctos.

## Problema real (de rendimiento, no de correctitud): exceso de I/O

`devbrowser.SaveGeometry` (invocado cada 2 s por el monitor) puede hacer hasta
**3 llamadas `Set()` por tick**, cada una reescribiendo el `.env` completo:

```
cada 2 s:
  Set(browser_position, ...)   → reescritura completa
  Set(browser_size, ...)       → reescritura completa
  Set(browser_position, ...)   → reescritura completa (sync con el tamaño)
```

= hasta 90 reescrituras completas del archivo por minuto. Innecesario.

Test que lo demuestra:
[write_count_test.go](../write_count_test.go) → cuenta las llamadas a `SetFile`.

## Solución propuesta: debounce automático (sin configuración)

El usuario pidió que el retardo sea **automático**, no configurable, para no
complicar el uso de la librería. Varias llamadas `Set()` dentro de una ventana
corta se agrupan en una sola escritura a disco.

### Diseño

`New()` activa el debounce con un valor por defecto fijo. No se expone ningún
método `SetDebounce`.

```go
const defaultDebounce = 150 * time.Millisecond

type TinyDB struct {
    // ... campos existentes ...
    debounceDelay time.Duration   // = defaultDebounce, fijado en New()
    debounceTimer *time.Timer
    dirty         bool
}
```

`Set()` deja de llamar a `persist()` directamente; usa `schedulePersist()`:

```go
func (t *TinyDB) schedulePersist() error {
    if t.debounceDelay == 0 {
        return t.persist()          // ruta sin debounce (compatibilidad)
    }
    t.dirty = true
    if t.debounceTimer == nil {
        t.debounceTimer = time.AfterFunc(t.debounceDelay, func() {
            t.mu.Lock()
            defer t.mu.Unlock()
            if t.dirty {
                t.persist()
                t.dirty = false
            }
            t.debounceTimer = nil
        })
    }
    return nil
}
```

- El valor en memoria se actualiza inmediatamente en `Set()` → `Get()` siempre
  devuelve el valor más reciente sin esperar.
- La escritura a disco se agrupa: N `Set()` en 150 ms → 1 sola escritura.
- `append()` (claves nuevas) NO cambia: sigue siendo escritura inmediata
  incremental, no entra al debounce.

### Garantía de flush

Se añade `Flush()` para forzar la escritura pendiente antes de que el proceso
termine (o cuando se necesite consistencia inmediata):

```go
func (t *TinyDB) Flush() error {
    // detiene el timer, escribe si hay cambios pendientes
}
```

### Impacto

| | Antes | Después (debounce 150 ms) |
|---|---|---|
| Escrituras por tick del monitor (2 s) | 3 | 1 |
| Escrituras por minuto | ~90 | ~30 |
| Correctitud de datos | ✓ | ✓ |
| Lectura `Get()` inmediata | ✓ | ✓ |
| Requiere `Flush()` al cerrar | no | sí (recomendado) |

## ¿Por qué 150 ms?

Suficiente para agrupar los 3 `Set()` de un mismo `SaveGeometry` (que ocurren en
microsegundos) y cualquier ráfaga de un manejador, pero corto frente al intervalo
del monitor (2 s), así que nunca se acumulan ticks. Imperceptible para el usuario.

## Archivos a cambiar

| Archivo | Cambio |
|---|---|
| [database.go](../database.go) | constante `defaultDebounce`; campos de debounce; fijarlo en `New()`; método `Flush()` |
| [methods.go](../methods.go) | `Set()` usa `schedulePersist()` en vez de `persist()` directo; añadir `schedulePersist()` |

## Tests

- [write_count_test.go](../write_count_test.go)
  - `TestDebounceCoalescesRapidWrites` — 3 `Set()` rápidos producen 1 sola
    escritura tras la ventana de debounce; el valor en memoria es inmediato.
  - `TestFlushWritesPendingState` — `Flush()` fuerza la escritura pendiente.
  - `TestRapidSetsPreserveAllValues` — integridad de datos (no se pierde nada).
- Tests existentes ([methods_test.go](../methods_test.go),
  [database_test.go](../database_test.go)) siguen pasando; `TestLogger` usa
  `Flush()` para forzar el `persist()` en la ruta de actualización.

## Integración (fuera de kvdb — DECIDIDO)

Quien instancie `kvdb` y maneje el ciclo de vida del proceso (la app) debe llamar
`db.Flush()` en el apagado para no perder una escritura pendiente dentro de la
ventana de 150 ms. Esto es responsabilidad del consumidor y se aborda en un
dispatch separado del repo `app` (fuera del alcance de este repo `kvdb`). Aquí
solo se expone `Flush()` y se documenta en el README/godoc.
