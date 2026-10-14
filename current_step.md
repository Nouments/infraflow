# INFRAFLOW — BLOC 3 : VALIDATION LAB DE L'EXÉCUTEUR CISCO IOS

Dépôt : Nouments/infraflow — branche : develop
Prérequis : le Bloc 2 est clos ou son statut est documenté. Sinon, arrête-toi et dis-le.
Ne commence pas le Bloc 4.

## OBJECTIF
Prouver, avec des preuves observées, que `cisco.ios.configure` fonctionne
sur un routeur Cisco IOS réel ou virtuel (GNS3). Sans équipement joignable,
aucun succès ne doit être déclaré.

## PÉRIMÈTRE INCLUS
1. Un runbook reproductible `docs/lab/cisco-ios-gns3.md` : topologie,
   adressage (ceux de README.md et examples/cisco-ios-lab.yaml, sans inventer),
   prérequis, commandes, résultats attendus.
2. Un script ou Makefile target de vérification pré-vol (joignabilité SSH,
   empreinte de clé d'hôte, version IOS) en lecture seule.
3. L'exécution, via l'API /api/v1/executions, de `cisco.ios.configure` pour :
   a) changement de hostname,
   b) configuration d'une interface Ethernet (IP/masque, admin-state),
   c) un cas d'échec volontaire (interface inexistante, CIDR invalide,
      mauvaise empreinte de clé d'hôte) qui doit être rejeté proprement.
4. Pour chaque cas : état avant (show running-config / show ip interface brief),
   requête envoyée, statut de l'exécution, résultat du read-back, état après,
   et vérification que les secrets n'apparaissent dans aucun log.
5. Correction des défauts confirmés par ce lab, avec un test de régression
   par défaut corrigé.

## PÉRIMÈTRE EXCLU (interdit)
- Autres fournisseurs (FortiGate, MikroTik, Proxmox).
- NAT, ACL, routage dynamique, VLAN/switchport.
- `write memory` automatique.
- Toute modification hors du lab isolé. Aucun équipement non autorisé.
- Aucun identifiant, token ou clé dans le dépôt, les logs ou les rapports.

## RÈGLES DE PREUVE
- Chaque commande est exécutée réellement et sa sortie est collée, avec son
  code de sortie. Rien n'est déduit ni supposé.
- Si l'équipement n'est pas joignable, écris NON EXÉCUTÉ, explique pourquoi,
  et livre le runbook pour que l'humain l'exécute. Ne simule rien.
- Les tests avec mock ne comptent pas comme preuve de lab.
- Si `git diff --check` ou un contrôle échoue, ne dis pas que c'est réussi.

## SÉCURITÉ GIT
Avant toute modification : git branch --show-current, git rev-parse HEAD,
git status --short --branch, git log -5 --oneline, git diff --check.
Préserve les modifications locales. Jamais de `git reset --hard` ni
`git clean`. Ne committe et ne pousse rien sans autorisation explicite.

## CRITÈRES D'ACCEPTATION
- go test -count=1 ./..., make fmt-check, make vet, make build : exit 0.
- Les 3 cas du lab (a, b, c) sont documentés avec preuves, ou marqués NON EXÉCUTÉ.
- Aucun secret dans les logs.
- INFRAFLOW_SPEC.md, ARCHITECTURE.md et CURRENT.md reflètent exactement
  l'état prouvé, pas l'état désiré.

## FORMAT DU RAPPORT FINAL
1. Décision : BLOC 3 COMPLET / INCOMPLET — PREUVES MANQUANTES
2. Tableau des cas : cas | avant | action | après | statut (PASS/FAIL/NON EXÉCUTÉ)
3. Défauts trouvés et corrigés (P0/P1/P2) avec le test de régression
4. Commandes exécutées avec leur code de sortie
5. Limites et risques résiduels
6. Score de maturité justifié
Arrête-toi là et attends l'approbation du PO/QA avant le Bloc 4.

## COMPTE RENDU — BLOC 3 (2026-10-14)

### Prérequis et environnement observé

- HEAD: `96904f08a91967b3c7fd307bbc996bec8f0e2b16`, branche `develop`.
- Le dernier statut du Bloc 2 enregistré dans `CURRENT.md` est incomplet (validation précédente à HEAD `3df0215`). Le `git diff --check` initial de cette passe à HEAD `96904f0` passe, mais la suite intégrale Bloc 2 n'a pas été rejouée sur ce HEAD. Bloc 3 a été limité au runbook et au pré-vol.
- Un processus local `gns3server` écoutait sur le port 3080; un processus QEMU observé était nommé `fortigate-1`, hors périmètre. `GET http://127.0.0.1:3080/v2/projects` a retourné HTTP 404. Aucun nœud Cisco autorisé, management IP ni empreinte SSH indépendante n'a été confirmé. Aucun accès SSH ni POST `/api/v1/executions` n'a été tenté.
- Les adresses README `192.168.0.2` (Agent) et `192.168.0.3` (Provider) ne sont pas des adresses IOS. `examples/cisco-ios-lab.yaml` utilise `192.0.2.10`, explicitement TEST-NET documentaire.

### Cas de lab

| Cas | Avant | Action | Après/read-back | Statut |
| --- | --- | --- | --- | --- |
| A — hostname | NON OBSERVÉ | NON ENVOYÉ | NON OBSERVÉ | NON EXÉCUTÉ |
| B — interface IPv4/admin-state | NON OBSERVÉ | NON ENVOYÉ | NON OBSERVÉ | NON EXÉCUTÉ |
| C — CIDR volontairement invalide | NON OBSERVÉ; API lab non démarrée | NON ENVOYÉ | NON OBSERVÉ | NON EXÉCUTÉ |

Cause: aucune cible IOS autorisée/joignable n'est identifiée. Aucun état avant/après ni scan de secrets dans les logs d'un run n'est revendiqué.

### Pré-vol et tests exécutés

| Contrôle | Commande | Code | Statut | Observation |
| --- | --- | ---: | --- | --- |
| Syntaxe pré-vol | `bash -n scripts/cisco-ios-preflight.sh` | 0 | PASS | Syntaxe seulement; aucune cible contactée. |
| Usage sans paramètres | `bash scripts/cisco-ios-preflight.sh` | 64 | PASS | Refus avant opération réseau. |
| Host invalide | `bash scripts/cisco-ios-preflight.sh 'not a host' test 'SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA'` | 64 | PASS | Rejet avant `ssh-keyscan`. |
| Fingerprint invalide | `bash scripts/cisco-ios-preflight.sh 192.0.2.10 test 'not-a-fingerprint'` | 64 | PASS | Rejet avant `ssh-keyscan`; aucune connexion TEST-NET. |
| Tests exécuteur/API | `go test -count=1 ./provider/internal/adapters/executors ./provider/internal/delivery/httpapi` | 0 | PASS | Tests automatisés, pas une preuve de lab. |
| Suite Go complète | `go test -count=1 ./...` | 0 | PASS | Tous les packages. |
| Formatage | `make fmt-check` | 0 | PASS | Cible existante. |
| Vet | `make vet` | 0 | PASS | Réussi. |
| Build | `make build` | 0 | PASS | Provider et Agent compilent. |

Le pré-vol n'a pas été exécuté avec une adresse/fingerprint réelle. Aucun secret réel n'a été fourni ou recherché dans les logs; cette exigence de preuve reste NON EXÉCUTÉE.

### Fichiers et limites

- `docs/lab/cisco-ios-gns3.md`: runbook; adresses connues séparées des valeurs opérateur inconnues; cas A/B/C explicitement `NON EXÉCUTÉ`.
- `scripts/cisco-ios-preflight.sh`: vérification SSH publique stricte, comparaison d'empreinte indépendante, lecture seule `show version`; seuls la syntaxe et les rejets d'entrées invalides ont été testés.
- `README.md`, `ARCHITECTURE.md`, `INFRAFLOW_SPEC.md`, `CURRENT.md` et `docs/cisco-ios-executor.md` reflètent l'absence de preuve IOS réelle.
- Aucun défaut de l'exécuteur n'a été confirmé par un lab; aucun code/test de l'exécuteur n'a été modifié.
- Bloc 1 reste incomplet; Bloc 2 reste documenté incomplet; Bloc 4 non commencé. Attendre une confirmation écrite PO/QA de la cible, son accès, son fingerprint, l'interface isolée et l'autorisation avant toute configuration.