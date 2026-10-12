# InfraFlow Current Status

Updated: 2026-10-12
Branch: `develop`
Base Commit: `b2c65cce4d0ad76c1514817e4de1285bc0d3856f`

## Implemented and verified in the repository

- Provider validation and deterministic planning are separate from generation and execution. Creating or retrying a job does not provision infrastructure.
- Planning jobs are persisted as private JSON records; the authenticated API supports list, read, cancel, and retry within the implemented status rules.
- A separate execution aggregate persists queued/running/succeeded/failed/cancelled/timed_out state, tasks, attempts, outputs, errors, actors, and transition history. It uses a typed executor port and the existing scheduler.
- Admin HTTP routes submit with idempotency, list with filters/pagination, read attempts/results/events, and request cancellation. The Provider registers two operational executors, `artifact.verify` (read-only SHA-256 integrity verification of catalog-published provider artifacts) and `cisco.ios.configure` (verified Cisco IOS Ethernet interface and hostname configuration via SSH with running-config read-back verification); all unregistered or unsupported task types are rejected instead of reporting a false success.
- Artifact generation publishes deterministic, hash-verified catalog entries. The `artifact.verify` task executor can cryptographically audit catalog artifacts in place without modifying files; `cisco.ios.configure` applies strictly typed configuration changes to authorized Cisco devices over SSH and verifies the resulting running-config without automatic startup-config persistence. Deployment task execution for other vendors (MikroTik, FortiGate, Proxmox) or complex network features (NAT, ACL, dynamic routing) is not implemented or validated.
- The Agent downloads published artifacts, verifies the stream and content hashes, stores them locally, processes generic inventory/topology, and reports artifact-level outcomes.
- The Agent has a local log outbox and can synchronize logs over the authenticated provider contract.
- The scheduler implements dependency graphs, global concurrency, resource locks, cancellation, retries, and timeouts. Provider startup wires it to the persistent execution service with `artifact.verify` and `cisco.ios.configure` as registered executors; other device or network deployment executors are rejected.
- Bootstrap artifacts can be generated. Agent DHCPv4, read-only TFTP, and bootstrap HTTP servers are separate explicit commands; they are not started by the normal agent run. There is no DNS daemon.
- Authenticated provider HTTP/gRPC APIs expose their existing planning, agent, event, and log records. The execution API is admin-only; administrators can inspect paginated execution lists, view task/attempt/error details, and request audited cancellation from both the web dashboard and the Linux TUI.
- Verification evidence documented in this repository reflects automated unit and integration tests passing against simulated or local filesystem state; it is distinct from operational validation in a physical or virtual network lab, which remains a separate deployment gate.

## Evidence from Task 1 (Fiabilisation des états TUI et des contrôles locaux)

- **Sous-tâche A — Stockage réel des artefacts (`StateDirectory`)** :
  - L'adaptateur réel `filesystem.StateStore` utilise directement `settings.Agent.StateDirectory` comme racine pour créer les dossiers `<site>/...` et `.infraflow-agent-state.json`. Il n'existe aucun sous-dossier intermédiaire `artifacts/`.
  - `inspectArtifactStore` inspecte fidèlement `StateDirectory` :
    - Répertoire inexistant : `Absent` (`Répertoire inexistant (non initialisé sur le disque)`).
    - Chemin pointant vers un fichier : `Dégradé` (`Le chemin configuré pointe vers un fichier au lieu d'un répertoire`).
    - Répertoire inaccessible : `Inaccessible` (`Erreur d'accès / refusé`).
    - Répertoire existant : `Accessible` (`Répertoire accessible en lecture ; capacité d'écriture non vérifiée`).
    - Intégrité : explicitement `Non auditée par le TUI (intégrité SHA256 validée par l'agent lors des réceptions gRPC)`.

- **Sous-tâche B — Statut véridique des outils Ansible et Terraform** :
  - La présence d'exécutables dans le `PATH` ne prouvant pas leur bon fonctionnement, les outils détectés dans `PATH` sans test d'exécution affichent désormais `Détecté` (`ansible-playbook et terraform détectés dans PATH (fonctionnement non vérifié)`).
  - Si un outil est manquant : `Dégradé` avec mention de l'exécutable manquant.
  - Si les deux sont manquants : `Absent` (`introuvables dans PATH`).
  - Aucun faux statut `Opérationnel` n'est affirmé sans exécution réelle.

- **Sous-tâche C — Clarification du cycle de vie de l'Outbox** :
  - `observability.NewOutbox` ne crée pas le fichier `pending-events.json` tant qu'aucun événement n'est persisté (`outbox.persist()`).
  - L'absence du fichier avant premier run est rapportée comme `Non initialisé` (`Fichier pending-events.json non créé (aucune file persistée sur disque)`), sans prétendre à tort que la file est vide.
  - Lorsque le fichier existe, son contenu est inspecté et décodé : `Opérationnel` (`File vérifiée (fichier vide, 0 événement en attente)` ou `N événement(s) en attente`), `Dégradé` si JSON corrompu, `Inaccessible` si illisible.

- **Sous-tâche D — Garantie des erreurs de configuration sur tous les modes CLI/TUI** :
  - Couverture systématique des modes `run`, `tui`, `execute-ansible` et `validate-terraform`.
  - Rejet systématique avec code de sortie non-nul (2) et message explicite sur `stderr` (avec masquage des secrets) pour :
    - Fichier de configuration absent / introuvable.
    - YAML syntaxiquement invalide.
    - Champ inconnu rejeté par `decoder.KnownFields(true)`.
    - Configuration incomplète rejetée par `Validate()`.

- **Sous-tâche E — Cycle de vie et résilience hors-ligne du TUI** :
  - Validation du cycle complet en 4 phases :
    1. Aucune donnée reçue au démarrage : `Indisponible (aucune donnée reçue)`, horodatage `Aucune synchronisation`.
    2. Première synchronisation réussie : `● Opérationnel`, affichage des données et de l'horodatage de rafraîchissement.
    3. Perte de connexion : conservation stricte des données en mémoire vive de session, maintien de l'horodatage initial réussi (sans inventer d'actualisation), affichage explicite de l'indisponibilité et de `Données en mémoire de session (non persistées sur disque)`.
    4. Reconnexion réussie : mise à jour des données, nouvel horodatage et rétablissement du statut `Synchronisé`.

- **Vérifications et Tests exécutés** :
  - `make fmt-check` : code 0, 100% conforme `gofmt`.
  - `make vet` : code 0, aucune erreur.
  - `make test-agent` : code 0, 100% des tests de l'agent passent.
  - `go test -count=1 ./...` : code 0, 100% des tests du dépôt passent sans cache.
  - `make build` : code 0, compilation réussie de `bin/infraflow-provider` et `bin/infraflow-agent`.

## Evidence from Task 1.1 (Correction de la sémantique du statut du stockage local)

- **Correction sémantique de `inspectArtifactStore()`** :
  - La vérification par `os.Stat()` et `os.ReadDir()` ne validant que l'existence et l'accès en lecture du répertoire sans tester la capacité d'écriture, le statut retourné est désormais `Accessible` (au lieu de `Opérationnel`).
  - Le détail associe explicitement le constat vérifié et la limite technique : `Répertoire accessible en lecture ; capacité d'écriture non vérifiée (N entrée(s) stockée(s))`.
  - Aucun fichier temporaire ou artefact fictif n'est créé pour simuler une écriture.
  - Vues TUI (`artifactStoreSummary()` et `servicesListView()`) : affichage du badge et du statut `Accessible` (palette Crisp Blue `runningStyle`), éliminant toute proclamation d'état `Opérationnel` pour le stockage sur la seule base d'une lecture.
  - Préservation stricte des états `Absent`, `Inaccessible`, `Dégradé` et `Non configuré`.
- **Tests unitaires adaptés et ajoutés (`agent/internal/delivery/tui/model_test.go`)** :
  - `TestArtifactStoreInspectionScenarios` : valide que les répertoires vides ou avec entrées produisent `Accessible`, rejette tout statut `Opérationnel`, et vérifie la mention `capacité d'écriture non vérifiée`.
  - `TestArtifactStoreRealAdapterIntegration` : valide la liaison avec le vrai `filesystem.NewStateStore` sous le statut `Accessible`.
  - `TestArtifactStoreViewsSummaryAndDetail` : valide la cohérence des vues `artifactStoreSummary`, `homeView` et `servicesListView` (aucun libellé `Opérationnel` pour le stockage local).
- **Vérifications réelles** :
  - `make fmt-check` : code 0.
  - `make vet` : code 0.
  - `go test -v infraflow/agent/internal/delivery/tui` : 38 tests réussis (0 échec).
  - `make test-agent` : code 0.
  - `go test -count=1 ./...` : code 0 sur 100% des packages du dépôt.
  - `make build` : code 0 (`bin/infraflow-provider` et `bin/infraflow-agent`).

## Evidence from Task 2 (Premier exécuteur réel, limité et vérifiable)

- **Implémentation de `artifact.verify` (`provider/internal/adapters/executors/artifact_verify.go`)** :
  - Premier exécuteur opérationnel d'InfraFlow implémentant `ports.ExecutionExecutor`.
  - Portée volontairement et strictement bornée : vérification d'intégrité cryptographique en lecture seule d'artefacts persistés dans le catalogue du Provider, sans modification de fichier, sans commande shell, sans script arbitraire et sans déploiement réseau sur équipement.
  - **Validation stricte des paramètres JSON** :
    - Contrat minimal : `{"path": "<site>/<relative-artifact-path>"}` (`ArtifactVerifyParameters`).
    - Rejet systématique des champs inconnus via `decoder.DisallowUnknownFields()`.
    - Rejet des chemins vides, absolus, contenant des séquences de traversée (`../`), des antislashs, des caractères de contrôle ou des segments non autorisés.
    - Vérification de la structure du chemin par rapport aux spécifications autorisées (`protocol.ValidArtifactPath`).
    - Aucune confiance accordée à un hash fourni par le demandeur ; la source de vérité est le manifeste catalogué (`OutputHash`).
  - **Exécution et intégrité** :
    - Résolution via l'interface `ports.ArtifactRepository.Open(path)` existante.
    - Lecture en flux borné (plafond strict à 32 MiB) avec calcul dynamique du condensat SHA-256 et vérification réactive de `ctx.Err()`.
    - Détection immédiate des altérations : si `actualHash != published.OutputHash`, la tâche échoue avec un diagnostic explicite mentionnant le hash attendu et le hash calculé.
    - Échec explicite si l'artefact est absent ou inaccessible (aucun faux succès).
    - **Résultat structuré persistant** (`ArtifactVerificationResult`) contenant : `path`, `type`, `input_hash`, `output_hash`, `actual_hash`, `size_bytes`, `verified: true`, et horodatage UTC RFC3339 `verified_at`.
    - Non-rejouable / non-retryable (`Retryable(err) == false`) car les erreurs de hash ou d'absence sont déterministes.
  - **Intégration dans la composition réelle du Provider** :
    - Enregistré dans `provider/internal/delivery/cli/cli.go` lors de l'initialisation de `application.NewExecutionService(..., []ports.ExecutionExecutor{artifactVerifier}, 4)`.
    - Le Provider accepte désormais les tâches `artifact.verify` et continue de rejeter tout type inconnu ou non enregistré (`ErrExecutionUnsupported`).

## Evidence from Task 3 (Premier exécuteur Cisco IOS réel via SSH)

- **Implémentation de `cisco.ios.configure` (`provider/internal/adapters/executors/cisco_ios_configure.go`)** :
  - Deuxième exécuteur opérationnel d'InfraFlow et premier adaptateur réseau réel implémentant `ports.ExecutionExecutor`.
  - Portée volontairement et strictement bornée : configuration du nom d'hôte (`hostname`), d'une ou plusieurs interfaces Ethernet (`description`, `ipv4_cidr`, `admin_state` up/down), et relecture/vérification stricte de la configuration réellement appliquée (`running-config`).
  - Aucune commande shell ni script arbitraire accepté en entrée ; seules des structures typées et validées sont traduites en commandes Cisco IOS CLI.
  - **Contrat des paramètres JSON (`CiscoIOSConfigureParameters`)** :
    - Format : `{"site": "<site>", "device_id": "<device>", "hostname": "<name>", "interfaces": [{"name": "<iface>", "description": "<desc>", "ipv4_cidr": "<ip/mask>", "admin_state": "up|down"}]}`.
    - Rejet systématique des champs inconnus via `decoder.DisallowUnknownFields()`.
    - Rejet des retours à la ligne (`\r`, `\n`) et caractères de contrôle dans les noms et descriptions.
    - Validation stricte des noms d'interfaces Ethernet Cisco (GigabitEthernet, FastEthernet, Ethernet, etc.) avec canonisation automatique des préfixes courts (`Gi0/0` -> `GigabitEthernet0/0`).
    - Validation stricte des adresses IPv4 CIDR (rejet d'IPv6, multicast, broadcast, loopback, 0.0.0.0).
    - Rejet des interfaces dupliquées (insensible à la casse sur nom canonisé).
    - Aucune adresse IP de destination arbitraire acceptée dans le payload de la tâche.
  - **Résolution sécurisée de cible et d'identifiants** :
    - Résolution de cible via `DeviceTargetResolver` (`ArtifactInventoryDeviceResolver`) inspectant `<site>/inventory.json` publié par le catalogue du Provider. Le site, les identités d'équipement, le constructeur Cisco et l'adresse IPv4 de gestion sont validés avant connexion; les cibles inconnues, ambiguës ou invalides sont rejetées. Le fichier n'est pas signé ni vérifié cryptographiquement par ce resolver.
    - Résolution des identifiants via `DeviceCredentialResolver` (`EnvDeviceCredentialResolver`) à partir des variables d'environnement (`INFRAFLOW_DEVICE_<SITE>_<DEVICE>_SSH_USER`, `..._SSH_PASSWORD`, etc.) ou d'un magasin sécurisé injectable. Aucun secret dans le payload de la tâche.
    - Épuration systématique de tout mot de passe, clé privée ou secret dans les messages d'erreurs, sorties et journaux (`sanitizeErrorWithCreds` et `observability.Redact`).
  - **Transport SSH et politique de clé d'hôte** :
    - Transport interactif borné (`SSHIOSTransport`) avec allocation de PTY VT100 et shell interactif pour gérer les invites IOS (`Router#`, `Router(config)#`, `Router(config-if)#`).
    - Désactivation de la pagination (`terminal length 0`, `terminal width 0`).
    - Politique de vérification de clé d'hôte stricte (`HostKeyVerifier`, `KnownHostsFileVerifier`, `FixedFingerprintVerifier`, `StaticPublicKeyVerifier`). Rejet de toute clé inconnue ou modifiée (`ErrHostKeyUnknown`, `ErrHostKeyMismatch`). Aucun recours à `InsecureIgnoreHostKey`.
    - Détection réactive de l'annulation (`ctx.Done()`) et des délais d'expiration fermant immédiatement la session TCP/SSH sous-jacente.
    - Détection des erreurs IOS explicites (`% Invalid input`, `% Incomplete command`, `% Bad IP address`, `% Configuration mode is locked`, etc.).
  - **Relecture et vérification obligatoire (Read-back verification)** :
    - Après application des commandes et sortie du mode configuration (`end`), l'exécuteur relit l'état actif via `show running-config | include ^hostname` et `show running-config interface <name>`.
    - Comparaison stricte entre l'état observé et l'état demandé (nom d'hôte, description, IP, masque, statut administratif).
    - La tâche n'est marquée `succeeded` **qu'après vérification positive intégrale**. Si une divergence est constatée, la tâche échoue avec un diagnostic explicite.
  - **Politique de persistance** :
    - Par défaut, aucune commande `write memory` ou `copy running-config startup-config` n'est exécutée.
    - Un avertissement explicite est inclus dans le résultat : `"Configuration was applied to running-config and verified; startup-config was not persisted (write memory disabled by policy)"`.
- **Intégration Provider** :
  - Enregistré dans `provider/internal/delivery/cli/cli.go` aux côtés de `artifact.verify` dans `ExecutionService`.
  - Les tâches `cisco.ios.configure` passent par le scheduler, la persistance `filesystem.ExecutionStore`, les tentatives et les audits existants.
- **Validation GNS3 en laboratoire réel** :
  - **NON EXÉCUTÉ** : Aucun équipement Cisco IOS physique ou virtuel n'a été utilisé pour cette validation. Les tests automatisés ne remplacent pas un test IOS réel; aucun résultat de laboratoire n'est simulé ou inventé.
  - Procédure de validation GNS3 : `docs/cisco-ios-executor.md`.
- **Tests unitaires et d'intégration exécutés** :
  - `provider/internal/adapters/executors/cisco_ios_configure_test.go` : 17 tests couvrant 100% des cas requis :
    - `TestCiscoIOSConfigureValidParameters` : validation et canonisation des paramètres conformes.
    - `TestCiscoIOSConfigureUnknownFieldsAndInvalidJSON` : rejet des champs inconnus, trailing JSON, JSON malformé.
    - `TestCiscoIOSConfigureInvalidIPv4` : rejet de préfixes invalides, IPv6, multicast, broadcast, loopback.
    - `TestCiscoIOSConfigureDuplicateOrMalformedInterfaces` : rejet des doublons et des noms malformés.
    - `TestCiscoIOSConfigureInvalidDescription` : rejet des caractères de contrôle et retours à la ligne.
    - `TestCiscoIOSConfigureUnknownTarget` : rejet des équipements non déclarés comme cibles autorisées.
    - `TestCiscoIOSConfigureCredentialsUnavailable` : échec sécurisé si les identifiants ne sont pas configurés.
    - `TestCiscoIOSConfigureSSHAuthFailure` : traitement de l'échec d'authentification SSH.
    - `TestHostKeyVerificationRules` : rejet d'une clé d'hôte inconnue ou altérée.
    - `TestCiscoIOSConfigureCancellationAndTimeout` : respect immédiat de l'annulation de contexte.
    - `TestCiscoIOSConfigureExplicitIOSError` : détection des erreurs syntaxiques ou logiques IOS.
    - `TestCiscoIOSConfigureConnectionInterrupted` : gestion de la réinitialisation de connexion TCP.
    - `TestCiscoIOSConfigurePartialResultDetection` : comptabilisation exacte des commandes appliquées avant échec.
    - `TestCiscoIOSConfigureVerificationMismatch` : échec explicite en cas de divergence lors de la relecture.
    - `TestCiscoIOSConfigureVerifiedSuccess` : succès conditionné par la vérification positive.
    - `TestCiscoIOSConfigureNoSecretsInErrorsOrLogs` : garantie d'absence totale de secrets dans les erreurs et sorties.
    - `TestCiscoIOSConfigureIntegrationWithExecutionService` : soumission, planification, exécution, audit et persistance complètes via `ExecutionService`, avec coexistence éprouvée de `artifact.verify` et rejet des types non enregistrés.
  - `provider/internal/infrastructure/credentials/device_test.go` : tests de résolution statique et via environnement.
- **Vérifications réelles** :
  - `make fmt-check` : code 0.
  - `make vet` : code 0.
  - `go test -count=1 ./provider/internal/adapters/executors` : 17 tests réussis (0 échec).
  - `go test -count=1 ./...` : code 0 sur 100% des packages du dépôt.
  - `make build` : code 0 (`bin/infraflow-provider` et `bin/infraflow-agent`).

## Evidence from Task 3.2 (Préparation de validation IOS réelle)

- **Exemple suivi** : `examples/cisco-ios-lab.yaml` contient un seul site et une cible Cisco IOS, sans identifiant ni secret. L'adresse `192.0.2.10` est explicitement réservée à la documentation (TEST-NET-1) et doit être remplacée dans une copie locale avant tout usage réel.
- **Vérification réelle du CLI** : `bin/infraflow-provider validate -f examples/cisco-ios-lab.yaml` a retourné `Valid: 1 site(s), 1 device(s); no infrastructure changes made.`. `bin/infraflow-provider generate -f examples/cisco-ios-lab.yaml -out <répertoire temporaire>` a produit `cisco-lab/inventory.json` et `topology.json`; une vérification `jq` a confirmé site `cisco-lab`, équipement `R1` et valeur documentaire `192.0.2.10`. Aucun inventaire réel ni résultat IOS n'a été généré.
- **Démarrage de configuration Provider** : le Provider a démarré avec un `artifact_directory` temporaire, API HTTP `127.0.0.1:8080` et gRPC `127.0.0.1:50051`, créé le bootstrap admin local et s'est arrêté proprement sur SIGINT; code de sortie 0. Aucun appel HTTP/SSH de lab n'a été fait. Le répertoire temporaire a été supprimé.
- **Contrats vérifiés dans le code et les tests** : génération via la commande `generate`; API admin locale optionnelle activée par `api_listen_address`; login `POST /api/v1/auth/login`; soumission `POST /api/v1/executions` avec bearer admin et `Idempotency-Key`; lecture `GET /api/v1/executions/{id}` et résultat dans `tasks[].result`. Le Provider génère le credential admin au premier bootstrap d'une base vide et écrit le script de lecture configuré.
- **Vérifications automatisées exécutées depuis la racine** : `make fmt-check`, `go test -count=1 ./provider/internal/adapters/executors`, `go test -count=1 ./provider/internal/application/...`, `go test -count=1 ./...`, `make vet`, `make build` et `git diff --check` ont chacun terminé avec code 0.
- **Accessibilité GNS3 de cet environnement** : **NON EXÉCUTÉ**. Aucun processus GNS3 ni socket local correspondant n'a été détecté; `127.0.0.1:3080` a refusé la connexion. Aucun endpoint distant de lab, équipement IOS, secret ou cible autorisée réel n'était fourni; aucune adresse Cisco n'a été contactée. Les tests unitaires/API simulés ne remplacent pas la validation IOS réelle.
- **Procédure reproductible** : [docs/cisco-ios-executor.md](docs/cisco-ios-executor.md) précise la validation de clé par canal indépendant, le setup loopback du Provider, le bootstrap et login admin, la mutation de description sans adresse IP, la restauration, la capture des réponses API réelles et un échec d'hôte inconnu avant mutation avec une tentative. Le fichier GNS3 historique sous `dev-lab/` est ignoré par Git et n'est pas une source versionnée.
- **Limite restante** : exécuter la procédure sur un routeur IOS dédié et isolé dans GNS3, capturer son état initial/final et son JSON d'exécution réel, puis archiver les preuves hors du dépôt sans secret.

## Remaining work

- Valider l'exécuteur `cisco.ios.configure` sur un routeur Cisco IOS réel ou virtuel dans un banc d'essai GNS3 / EVE-NG selon la procédure définie dans `docs/cisco-ios-executor.md` (**NON EXÉCUTÉ**).
- Implémenter ultérieurement une tâche explicite de persistance de configuration (`cisco.ios.save` / `write memory`).
- Implémenter les autres adaptateurs d'équipements réseau (FortiGate, MikroTik, Proxmox) uniquement après définition de leurs schémas stricts et preuves d'exécution.
- Implémenter ou différer explicitement le service DNS.

## Next implementation task

Exécuter la procédure de validation GNS3 sur un équipement Cisco IOS de laboratoire réel et consigner les preuves d'état observé (sorties de terminal réelles) sans simulation.