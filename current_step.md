# STEP 01.5 — CORRECTIF FINAL DU PLANNER

## Objectif

Terminer le planner existant dans `Nouments/infraflow`, branche `develop`, sans réécriture inutile ni nouveau système parallèle.

Fichiers prioritaires :

* `internal/domain/plan.go`
* `internal/application/planner/planner.go`
* `internal/application/planner/planner_test.go`
* `internal/domain/model.go`, uniquement si nécessaire à l'intégration du lifecycle.

## 1. Identifiant déterministe du plan

Le `Plan.ID` actuel dépend essentiellement des noms de sites. Il ne distingue donc pas deux états désirés différents pour un même site.

Corriger ce problème :

* Définir une représentation canonique et déterministe de l'intention désirée.
* Calculer un hash stable à partir de cette représentation.
* Exclure timestamps, UUID aléatoires, données d'exécution et observations.
* Documenter précisément ce que l'identité du plan représente.
* Garantir que le même état désiré produit le même ID, indépendamment de l'ordre des sites et des équipements dans les entrées.
* Garantir que deux intentions différentes produisent des IDs différents dans les tests.
* Éviter les collisions ambiguës liées à la concaténation de chaînes.

Ne pas confondre l'identité du plan avec les IDs des tâches. Conserver des IDs de tâches déterministes et lisibles.

## 2. Lifecycle réel

Inspecter tous les usages de `LifecycleState`, `Transition(StatePlanned)` et `planner.Build`.

Le planning doit faire passer à `PLANNED` les objets du domaine qui possèdent effectivement un lifecycle et qui ont été planifiés.

Ne pas ajouter artificiellement un lifecycle à chaque objet si l'architecture actuelle ne le prévoit pas. Si le modèle ne permet pas de conserver cet état, proposer la plus petite évolution cohérente et l'intégrer seulement après vérification des consommateurs.

Après planning, vérifier explicitement :

* Desired = true
* Planned = true
* Generated = false
* Executed = false
* Verified = false
* Observed = false

Ne jamais transformer une observation `INFERRED` en `OBSERVED`.

Si `Build` doit rester une fonction pure retournant un nouveau plan, préserver cette propriété et choisir une intégration du lifecycle compatible avec les conventions du projet. Ne pas modifier silencieusement l'état d'entrée.

## 3. Actions

L'action prise en charge dans cette étape reste :

`configure_interfaces`

Créer cette action uniquement si une configuration désirée d'interfaces existe.

Ne pas créer automatiquement :

* generate_inventory
* generate_topology
* provision_device
* NAT
* DHCP
* VLAN
* routing
* OSPF

Les rôles `wan`, `lan`, `transit` et `management` restent descriptifs. Ils ne déclenchent aucune fonctionnalité implicite.

Préserver la méthode explicitement configurée. Si elle est absente, utiliser le profil connu seulement lorsque cette résolution est cohérente avec le modèle existant. Ne jamais présenter une méthode connue comme vérifiée.

## 4. Dépendances et artefacts

Conserver les champs existants du modèle.

Ne déclarer aucune dépendance qui ne correspond pas à une véritable relation entre tâches. Ne référencer aucun artefact simplement supposé exister.

Si aucun artefact généré n'est disponible, laisser `Artifacts` vide. Ne pas appeler le générateur depuis le planner.

## 5. Tests obligatoires

Ajouter ou adapter des tests pour vérifier :

1. Un site vide produit un plan `PLANNED` sans tâche.
2. Un équipement avec des interfaces produit une seule tâche `configure_interfaces`.
3. Un équipement sans interfaces ne produit aucune tâche.
4. Le tri des sites et équipements est stable.
5. Les IDs des tâches sont déterministes.
6. L'ID du plan est identique pour le même état désiré, même si l'ordre des entrées change.
7. Une modification de l'intention désirée modifie l'ID du plan.
8. La méthode `netconf`, `api` ou `https` n'implique aucune vérification.
9. Le planning ne marque pas `Generated`, `Executed`, `Verified` ou `Observed`.
10. Le rôle `wan` ne génère pas de NAT.
11. Aucun artefact ou dépendance fictif n'est ajouté.
12. Le planner ne réalise aucune connexion réseau ni exécution de commande.

Ajouter des tests de cas limites pour la canonicalisation et les identifiants.

## 6. Périmètre interdit

Ne pas développer dans cette étape :

* exécution SSH/NETCONF/RESTCONF ;
* connexion aux équipements ;
* ZTP ou Cisco autoinstall ;
* NAT, routage, OSPF ;
* PXE/iPXE, DHCP, TFTP, FTP ;
* Proxmox ;
* moteur d'exécution, retry, parallélisme ou scheduler ;
* nouvelle interface Web/TUI ;
* nouveau système de lifecycle ou de capability en parallèle.

Ne pas modifier inutilement les générateurs Ansible, les profils Cisco/MikroTik/FortiGate ou le moteur de comparaison du drift.

## 7. Validation réelle

Exécuter depuis la racine du dépôt :

```bash
gofmt -w internal/domain/plan.go internal/application/planner/planner.go internal/application/planner/planner_test.go
go test ./... -count=1
git diff --check
git status --short
git diff --stat
git diff
```

Adapter la commande `gofmt` si d'autres fichiers Go sont effectivement modifiés.

Ne déclarer les tests réussis que si leur commande a réellement été exécutée et que son résultat est disponible.

Ne pas créer de commit si les tests échouent.

## 8. Rapport final obligatoire

Retourner :

* fichiers modifiés ;
* décision sur l'identité du plan ;
* méthode de canonicalisation et hash ;
* intégration effective du lifecycle ;
* tests ajoutés ;
* résultat exact de `go test ./... -count=1` ;
* résultat de `git diff --check` ;
* limites restantes ;
* SHA du commit, uniquement s'il existe réellement.

Respecter en permanence :

MOCK != REAL
TODO != DONE
GENERATED != EXECUTED
EXECUTED != VERIFIED
VERIFIED != LAB-TESTED
INFERRED != OBSERVED

Ne pas annoncer le Step 01.5 terminé avant d'avoir vérifié ces critères.
