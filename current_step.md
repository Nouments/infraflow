# InfraFlow — Cycle d’amélioration de génération déclarative

## Publication fiable — 2026-11-14

- Branche `develop`; HEAD de départ inspecté :
  `228414a0ea267efbbe07b05f29616d538dfdbd4a`. Changements locaux non commités,
  aucun push.
- Le pipeline `GenerateAllContext` génère d’abord en staging. Il valide les
  manifestes/catalogues antérieurs, prend une sauvegarde des seuls fichiers
  catalogués, retire uniquement les artefacts catalogués devenus obsolètes,
  puis publie les nouveaux artefacts, manifestes de site, rapport global et
  manifeste racine en dernier. Les fichiers non catalogués et répertoires
  vides sont conservés.
- En cas d’échec d’écriture, suppression ou annulation, le pipeline tente de
  restaurer les fichiers sauvegardés. `Catalog` refuse la lecture pendant une
  génération et en présence de données de récupération. Ce mécanisme n’est
  pas un échange atomique global : une panne du processus ou de la machine peut
  laisser un verrou ou des données de récupération à traiter manuellement.
- Tests de régression ajoutés : désactivation DHCP, suppression d’un appareil,
  génération invalide, panne de publication injectée, traversal/symlink,
  idempotence et conservation de fichiers externes. Le paquet génération a
  réussi avant les dernières retouches de durcissement; le dernier lancement
  ciblé a été bloqué par Application Control avant l’exécution des tests.
- Commandes finales : `go test ./internal/adapters/generation ./pkg/protocol -count=1`
  bloqué par Application Control; `go test ./...` échoue avec ces blocages,
  SQLite sans CGO, Terraform absent, interfaces réseau et hypothèses de
  permissions/environnement Windows. `go vet ./...`, `go build ./provider/cmd ./agent/cmd`
  et `git diff --check` réussissent. Environnement : Windows/amd64,
  `CGO_ENABLED=0`, `CC=gcc`, aucun `gcc` détecté.
- Le run CI [43615857215](https://github.com/Nouments/infraflow/actions/runs/43615857215)
  porte sur le HEAD distant inchangé, pas sur ces changements locaux. Linux
  échoue dans le test Agent multisite qui attend les artefacts bootstrap
  désactivés; Windows échoue entre autres au démarrage de scripts, à cause de
  Terraform absent, de tests d’interfaces/permissions et d’une assertion de
  chemin. Aucun résultat CI Linux/Windows ne valide ce patch.
- Disponibilité non démontrée tant que les tests ciblés ne peuvent pas être
  exécutés et qu’un run CI vert n’a pas validé la révision modifiée. Aucune
  validation sur équipement ou en laboratoire.

## État de stabilisation — 2026-11-12

- Branche `develop`; HEAD inspecté au départ :
  `986d2a7b15fe9aee46f4579688b694862dd64485`. Les corrections sont locales et
  non commitées; aucun fichier n’a été staged, aucun push effectué.
- Les commandes Provider `validate`, `plan` et `generate-all` ont été lancées
  pour `examples/provisioning-generation.yaml`, `examples/infra.yaml` et
  `examples/multisite-preparation.yaml`, dans des sorties temporaires neuves.
  Les trois validations et générations ont réussi.
- Résultats : démo de provisioning, **30 fichiers**, 2 sites et 13 artefacts
  catalogués par site; `infra.yaml`, **19 fichiers**, 1 site et 16 artefacts
  (dont 5 artefacts DHCP/TFTP/PXE pour les services explicitement activés);
  multisite, **50 fichiers**, 4 sites et 11 artefacts par site. Les manifestes
  et rapports racine/site sont du JSON valide. Fichiers catalogués, tailles et
  SHA-256 concordent; aucun fichier site non catalogué détecté. Cisco `.cfg`
  et MikroTik `.auto.rsc` présents; pas de bootstrap sans service activé.
- SHA-256 de l’artefact `.auto.rsc` :
  `237b12b4ea2724d36763d9f29a8d200c37b20a685ea76e68e1158b1a58a984fc`.
  L’intention OSPF non supportée échoue avec code non nul et ne crée aucun
  répertoire de sortie. Aucun équipement n’a été contacté.
- Les tests existants couvrent les échecs partiels multisite et le transfert
  Agent limité au site avec vérification de hash, mais ils n’ont pas abouti
  comme preuves exécutées dans cette session Windows.
- Tests : `go test -count=1 ./internal/adapters/config ./internal/adapters/generation ./pkg/protocol`
  échoue globalement : config passe, Application Control bloque les exécutables
  de tests génération/protocole. `go test -count=1 ./provider/internal/...`,
  `go test -count=1 ./agent/internal/...` et `go test -count=1 ./...` échouent
  avec blocage Application Control, SQLite sans CGO et limites d’intégration
  Windows. `go vet ./...`, `go build ./provider/cmd ./agent/cmd` et
  `git diff --check` réussissent. `gofmt -l` direct liste tous les fichiers car
  le checkout Windows utilise CRLF et les blobs Git LF; le contrôle sur copies
  temporaires normalisées en LF réussit. Aucun changement massif de fins de
  ligne n’a été effectué.
- Environnement : Go `go1.27.3 windows/amd64`, `CGO_ENABLED=0`,
  `GOOS=windows`, `GOARCH=amd64`, `CC=gcc`; aucun exécutable `gcc` disponible.
  Le build réussi ne prouve donc pas le fonctionnement runtime de SQLite.
- Le run GitHub Actions [43145691432](https://github.com/Nouments/infraflow/actions/runs/43145691432)
  sur le HEAD inspecté a échoué sous Linux (driver SQLite non enregistré) et
  Windows (`go` absent du `PATH` dans le shell MSYS2). Les corrections locales
  restaurent l’import SQLite, passent les étapes Windows à PowerShell et
  désactivent la conversion CRLF du checkout Windows; aucun run CI ne valide
  ces changements non commités.
- `scripts/demo-provisioning.ps1` passe l’analyse syntaxique PowerShell; son
  lancement a été bloqué par la stratégie d’exécution locale, qui n’a pas été
  changée ni contournée. Les commandes CLI et vérifications ont été exécutées
  séparément.
- **Score de preuve de disponibilité : 7/10** : génération/intégrité 3/3;
  intention non supportée et multisite 1/2; build/contrôles statiques 2/2;
  tests complets et CI sur la révision modifiée 0/2; procédure/docs 1/1.
  Ce n’est pas un score de préparation à la mise en production.
  **Pas prêt pour release** tant que les tests ciblés/complets et la CI Linux
  ne réussissent pas sur la révision modifiée.
- Cisco `.cfg` = texte rendu, pas Cisco ZTP; MikroTik `.auto.rsc` = rendu non
  prouvé sur CHR ou matériel; FortiOS `.conf` = rendu uniquement. Aucun vendor
  n’est validé matériellement. Génération, livraison, exécution et vérification
  restent des étapes distinctes.

> Les relevés du 2026-11-11 et plus anciens ci-dessous sont historiques; ce
> relevé remplace leurs affirmations de tests, build et exécution CLI.

## Mise à jour de génération — 2026-11-11

- Branche `develop`; HEAD de départ `7d03baf681325b2a97c75de02ce51c9084839375`.
- Le pipeline `generate-all` rend maintenant un artefact RouterOS `.auto.rsc`
  via un template versionné pour le modèle déclaré RB750Gr3, le train RouterOS
  `7.x` et la version de template `1`. L’artefact reste `rendered_unverified`;
  aucune compatibilité, syntaxe réelle, livraison, exécution ou lecture
  `.auto.log` n’est affirmée.
- Cisco ZTP reste bloqué par l’exigence TLS-only et l’absence de profil
  modèle/release prouvé. AutoInstall Cisco et ZTP Fortinet restent non
  supportés. Le fichier Cisco de la démo est un rendu `.cfg`, pas du ZTP.
- `go test -count=1 ./internal/adapters/generation ./pkg/protocol` : PASS,
  y compris la lecture du YAML de démo, `GenerateAll`, la lecture du catalogue
  et les SHA-256 des fichiers Cisco `.cfg` et RouterOS `.auto.rsc`.
- `go test ./...` : FAIL. Un test de permissions Agent échoue sur Windows,
  Application Control bloque deux binaires de test Agent et le test multisite
  ne peut compiler Provider car `CGO_ENABLED=0` exclut les symboles cgo de
  SQLite. `go test -race ./...` exige CGO.
- `go vet ./...` et `go build ./provider/cmd ./agent/cmd` sont bloqués par les
  mêmes symboles SQLite absents. GNU `make` n’est pas installé; `gofmt -l` sur
  les fichiers Go modifiés est propre. La CLI Provider n’a pas pu être lancée;
  aucun répertoire de sortie persistant n’a été créé.
- Score de progression du sprint : **4,8/10**, pondéré entre intégration et
  manifestes (20 %, 8/10), profil RouterOS `.auto.rsc` (30 %, 8/10), Cisco ZTP
  (20 %, 0/10), AutoInstall Cisco (10 %, 0/10), traitement des profils ZTP
  Fortinet (10 %, 2/10) et preuves/démo/documentation (10 %, 6/10). Ce score
  concerne le sprint, pas la maturité globale d’InfraFlow.
- Aucun équipement contacté; aucun commit ni push effectué.

## Campagne de complétion et tests (2026-10-30)

- Commit de départ : `710b7ee5163acb3760c1e7043d7fa8589ca4fad3` sur `develop`;
  worktree propre au démarrage.
- Changement livré dans ce cycle : l’exécuteur Cisco SSH lit maintenant
  l’état courant des ressources demandées avant d’entrer en configuration.
  Une erreur de prélecture arrête le traitement avant toute mutation. Le
  rapport structuré distingue l’état préexistant de l’état post-application,
  lequel reste soumis à une lecture et une comparaison réussies.
- Limite conservée : aucune nouvelle capacité ZTP/AutoInstall, RouterOS API
  ou FortiOS API n’a été déclarée ou intégrée pendant cette campagne. Le
  protocole Cisco ZTP consulté propose un URL HTTP dans l’exemple option 67;
  HTTPS pour la cible retenue n’a pas été vérifié et le choix TLS-only reste
  bloquant pour la génération.
- Vérifications : `go test ./...`, `go test -race ./...`, `go vet ./...`,
  `go test -cover ./...`, `go build ./provider/cmd ./agent/cmd`,
  `make fmt-check`, `bash scripts/cisco-ios-preflight_test.sh`,
  et le test multisite ciblé ont réussi. La couverture globale des statements
  est de 66,1 % (`go tool cover` sur le profil agrégé).
- Le test race a échoué une première fois sous charge concurrente dans le test
  de limite de sortie SSH (timeout au lieu de l’erreur attendue); il a réussi
  isolément puis dans le rejeu complet `go test -race ./...`. Aucun résultat
  matériel/virtuel : IOS XE, CHR et FortiGate VM n’ont pas été contactés.
- Le compte rendu détaillé de cette campagne est
  [docs/COMPLETION_AND_TEST_REPORT.md](docs/COMPLETION_AND_TEST_REPORT.md).

> Mise à jour du 2026-10-29 : ce journal contient le relevé historique des
> changements de génération, mais ses évaluations antérieures ne sont pas un
> score courant après la décision de retirer Ansible. Les générateurs,
> artefacts, runners et commandes Ansible ont été supprimés. La priorité
> d’architecture est désormais le ZTP et l’AutoInstall par constructeur; les
> contrats bootstrap existants ne sont pas encore des workflows ZTP/AIA
> vérifiés.

## État

- Date : 2026-10-29
- Branche : `develop`
- Commit de référence : `1107f4373439e0a34c3efb4cbd8e1fb9e39d2392`
- Travail local non commité; aucun push ni merge.
- Score du cycle précédent : **6,33/10**. Score après le cycle précédent : **6,40/10**; score courant recalculé : **6,48/10**. Les tests locaux sont verts, mais le seuil 10/10 n’est pas atteint.

## Objectif et changements de ce cycle

- Rejeter le mélange des clés racines `site` et `sites`, y compris quand `sites` est vide.
- Préserver `os_version` dans l’adaptation du schéma `NetworkDeployment` vers le modèle partagé.
- Valider la joignabilité directe des next-hops statiques : ils doivent être dans le préfixe IPv4 statique choisi ou dans un préfixe statique directement connecté; l’adresse locale, réseau et broadcast ne sont pas acceptés comme next-hop.
- Rejeter les préfixes statiques qui se chevauchent sur les interfaces d’un même équipement, les extrémités répétées/réutilisées entre liens, et exiger que le préfixe IPv4 de chaque extrémité corresponde au réseau déclaré du lien.
- Préserver `os_version` lors de l’adaptation `NetworkDeployment` → modèle partagé; valider strictement la forme de version RouterOS 7 acceptée par le renderer.
- Corriger le rendu de destination FortiOS en adresse et masque séparés.
- Rejeter les destinations de routes qui ne sont pas des réseaux IPv4 canoniques, pour garantir un rendu cohérent entre constructeurs.
- Générer les routes statiques Cisco via `cisco.ios.ios_static_routes`; vérifier le schéma contre la source officielle de la collection taguée 11.5.1 et tester le YAML réellement écrit, notamment destination CIDR, next-hop, interface et distance.
- Renseigner `7.x` dans le pilote MikroTik comme train de version, en précisant que le build et la compatibilité modèle/version restent inconnus; ajouter un test de génération du pilote vérifiant ses trois artefacts natifs et les hashes catalogués.
- Rejeter les IDs d’artefacts natifs en collision sans distinction de casse, afin d’éviter des écrasements sur des systèmes de fichiers insensibles à la casse.

## Matrice de couverture

| Domaine | État | Preuve exécutée | Limite principale |
|---|---|---|---|
| YAML mono-site/multisite et validation réseau | Implémenté, testé | Tests `config`; les formes `site` et `sites`, liens uniques, préfixes qui se chevauchent et next-hops sont testés | Routage récursif, VLAN, protocoles dynamiques et routage inter-sites non modélisés |
| Cisco, MikroTik, Fortinet natifs | Partiel, testé | Tests générateurs et `generate-all` du pilote produisent trois artefacts natifs hashés | Famille seulement; pas de matrice modèle/version ni de validation syntaxique constructeur |
| Ansible | Retiré | Code, contrats d’artefacts, CLI, exécution Agent et rendu Provider supprimés | Les fichiers historiques de démonstration sont supprimés et l’architecture n’en dépend plus |
| ZTP / AutoInstall | Non implémenté comme workflow constructeur | Artefacts bootstrap génériques et services DHCP/TFTP/HTTP existants | Protocoles, formats, confiance, modèle/version et résultats de provisioning spécifiques restent à concevoir et valider |
| ZTP / AutoInstall | Absent comme flux constructeur | Bootstrap générique existant, non présenté comme ZTP validé | Prérequis et procédures exactes par plateforme/modèle/version manquants |
| Catalogue matériel local | Partiel | Catalogue YAML embarqué et contrôles de ressources locales existants | Pas de synchroniseur; aucune ressource exacte et aucun droit de redistribution établis |
| Multisite / Agents | Implémenté sur le périmètre actuel | Tests d’intégration Provider-Agent et gardes de manifestes/site de la suite complète | Capacité maximale de génération fixe; la couverture future dépend de manifestes publiés |
| Sécurité / intégrité | Solide, testé | Scoping des artefacts, checksums, publication et chemins sûrs couverts par tests existants | Pas d’audit matériel, de validation externe ni de preuve de déploiement |
| Tests / documentation | Solide pour le périmètre testé | Suite complète, contrôles et génération du pilote réussis | Aucune validation sur équipement ou syntaxe native externe |

## Auto-évaluation pondérée

| Critère | Poids | Note /10 | Justification synthétique |
|---|---:|---:|---|
| A. Contrat YAML et validation réseau | 1,5 | 8,5 | Mono/multisite, identifiants, interfaces, liens uniques, préfixes et next-hops directement connectés testés; fonctions L2/L3 avancées absentes |
| B. Générateurs natifs et routage | 2,0 | 4,5 | Trois profils rendent hostname, IPv4 et routes statiques; modèle/version et syntaxe restent non certifiés, VLAN/OSPF refusés |
| C. Provisioning ZTP et AutoInstall | 1,5 | À rebaseliner | La génération bootstrap actuelle ne constitue pas un workflow ZTP/AutoInstall et aucun outil d’automatisation externe n’est retenu |
| D. Mono-site, multisite et Agents | 1,5 | 8,0 | Publication indépendante, manifestes hashés, succès partiel et isolation Agent/site testés |
| E. Catalogue matériel local | 1,0 | 3,0 | Catalogue local embarqué présent, mais peu fourni; aucune synchronisation ni ressource binaire vérifiée |
| F. Sécurité et intégrité | 1,0 | 8,5 | Autorisation par site, validation des chemins, checksums et publication atomique couverts |
| G. Tests et preuves | 1,0 | 8,5 | Suite Go et pilotes reproductibles réussis; pas de preuve constructeur/matériel |
| H. Documentation et reproductibilité | 0,5 | 8,5 | Limites et commandes documentées; support reste à réduire à des matrices certifiées |

Le score de 6,48/10 ci-dessous appartient au cycle antérieur à la suppression
d’Ansible et n’est pas repris comme évaluation du périmètre actuel. Un nouveau
score devra être établi selon les critères d’acceptation ZTP/AutoInstall.

## Fichiers modifiés ce cycle

- `README.md`
- `demo-output/pilot-input.yaml`
- `internal/adapters/config/load.go`
- `internal/adapters/config/load_test.go`
- `internal/adapters/config/deployment.go`
- `internal/adapters/config/deployment_test.go`
- `internal/adapters/generation/generator_test.go`
- `internal/adapters/generation/native.go`
- `internal/adapters/generation/native_test.go`
- `internal/adapters/generation/vendor_ansible.go` (supprimé)
- `internal/adapters/generation/vendor_ansible_test.go` (supprimé)
- `current_step.md`

## Vérifications réellement exécutées

- `go test -count=1 ./...` : réussi après l’intégration du rendu Cisco routes.
- `go test -race -count=1 ./internal/adapters/config ./internal/adapters/generation` : réussi.
- `make fmt-check` : réussi.
- `make vet` : réussi.
- `make build` : réussi, Provider et Agent compilés.
- `make test-scripts` : réussi, contrôles de préflight Cisco passés.
- CLI : validation du pilote réussie (`1 site`, `3 devices`); `generate-all` a produit les manifestes racine/site et trois configs natives dans un répertoire temporaire, puis ce répertoire a été supprimé.
- `git diff --check` : réussi.
- Après retrait, aucune exécution ou validation Ansible n’est requise par le projet.
- Vérificateur Terraform : `terraform` doit être vérifié séparément avant d’annoncer une validation de ses artefacts.

## Blocages et prochaine action

La note de 10/10 n’est pas justifiable dans cet environnement : aucune cible matérielle/lab n’a été autorisée, aucun validateur syntaxique constructeur n’a été exécuté, et les ressources exactes/licences nécessaires au catalogue ne sont pas établies. Les versions et fonctionnalités non prouvées demeurent explicitement non vérifiées ou refusées; aucune configuration n’a été envoyée à un équipement.

Pour poursuivre vers 10/10 sans inventer de compatibilité, il faut obtenir des cibles modèle/version et des sources officielles/validateurs adaptés pour chaque profil, un lab autorisé, ainsi que des URL d’assets et droits de redistribution vérifiables. Puis ajouter les tests correspondants et réévaluer les huit critères.
