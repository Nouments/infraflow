# InfraFlow — Finalisation Step 01.5

Travaille sur la branche `develop` du dépôt `Nouments/infraflow`.

## Objectif

Finaliser le planner actuel sans réécriture générale ni ajout de nouvelles fonctionnalités.

### Tâches

1. Supprimer le bloc inutile `if method == "" { method = "" }` dans `internal/application/planner/planner.go`.
2. Examiner la gestion d’erreur de `LifecycleState.Transition(StatePlanned)`. Éviter un `panic` inutile, sans modifier silencieusement le contrat de `Build()` ni introduire une architecture parallèle.
3. Conserver l’identifiant du plan fondé sur SHA-256 et la canonicalisation déterministe.
4. Vérifier que l’ordre des entrées ne change pas le hash, qu’une modification de l’état désiré le change, et que les identifiants de domaine exclus ne modifient pas le hash.
5. Ne pas ajouter de fonctionnalités réseau, NAT, DHCP, OSPF, ZTP ou d’exécution d’équipements dans cette étape.

### Validation obligatoire

Depuis la racine du dépôt, exécuter réellement :

```bash
gofmt -w internal/application/planner/planner.go internal/application/planner/planner_test.go internal/domain/plan.go
go test ./... -count=1
git diff --check
git status --short
git diff --stat
git diff
```

Adapter `gofmt` aux fichiers effectivement modifiés.

Ne pas annoncer de tests réussis sans résultat réel. Ne pas créer de commit si les tests échouent.

### Rapport attendu

Retourner :

* les fichiers modifiés ;
* les corrections apportées ;
* la sortie exacte des tests ;
* le résultat de `git diff --check` ;
* les limites restantes ;
* le SHA du commit, uniquement si un commit a réellement été créé.

Rappels : `GENERATED != EXECUTED`, `EXECUTED != VERIFIED`, `VERIFIED != LAB-TESTED`, `INFERRED != OBSERVED`.
