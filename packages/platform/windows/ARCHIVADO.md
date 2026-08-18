# Código archivado

`ipc.go` e `ipc_test.go` implementan un Named Pipe con descriptor de seguridad
SDDL. Se escribieron y se probaron en la Fase 0: funcionan, y las tres pruebas
pasaban (ida y vuelta del propietario, rechazo del segundo servidor, ausencia
de puerto TCP).

**No se usan.** La arquitectura v2 elimina el demonio persistente, así que no
hay proceso con el que hablar: la extensión lee la base local directamente.

Se conservan con extensión `.archivado` para que no compilen y para dejar
constancia de que la decisión fue deliberada, no un olvido. Si alguna vez
vuelve un proceso residente —ver el punto débil 1 del plan—, están aquí.

Ver `docs/PLAN.md` §2.
