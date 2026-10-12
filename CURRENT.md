# InfraFlow Current Status

Updated: 2026-10-12
Branch: `develop`
Base Commit: `b2c65cce4d0ad76c1514817e4de1285bc0d3856f`

## Implemented and verified in the repository

- Provider validation and deterministic planning are separate from generation and execution. Creating or retrying a job does not provision infrastructure.
- Planning jobs are persisted as private JSON records; the authenticated API supports list, read, cancel, and retry within the implemented status rules.
- A separate execution aggregate persists queued/running/succeeded/failed/cancelled/timed_out state, tasks, attempts, outputs, errors, actors, and transition history. It uses a typed executor port and the existing scheduler.
- Admin HTTP routes submit with idempotency, list with filters/pagination, read attempts/results/events, and request cancellation. The default Provider registers no executor and rejects unsupported types instead of reporting a false success.
- Artifact generation publishes deterministic, hash-verified catalog entries. Vendor artifacts remain marked unverified; execution and verification are not requested.
- The Agent downloads published artifacts, verifies the stream and content hashes, stores them locally, processes generic inventory/topology, and reports artifact-level outcomes.
- The Agent has a local log outbox and can synchronize logs over the authenticated provider contract.
- The scheduler implements dependency graphs, global concurrency, resource locks, cancellation, retries, and timeouts. Provider startup wires it to the persistent execution service; no device executor is registered.
- Bootstrap artifacts can be generated. Agent DHCPv4, read-only TFTP, and bootstrap HTTP servers are separate explicit commands; they are not started by the normal agent run. There is no DNS daemon.
- Authenticated provider HTTP/gRPC APIs expose their existing planning, agent, event, and log records. The execution API is admin-only; administrators can inspect paginated execution lists, view task/attempt/error details, and request audited cancellation from both the web dashboard and the Linux TUI.

## Evidence from Task 1 (Fiabilisation des états TUI et des contrôles locaux)

- **Sous-tâche A — Stockage réel des artefacts (`StateDirectory`)** :
  - L'adaptateur réel `filesystem.StateStore` utilise directement `settings.Agent.StateDirectory` comme racine pour créer les dossiers `<site>/...` et `.infraflow-agent-state.json`. Il n'existe aucun sous-dossier intermédiaire `artifacts/`.
  - `inspectArtifactStore` inspecte fidèlement `StateDirectory` :
    - Répertoire inexistant : `Absent` (`Répertoire inexistant (non initialisé sur le disque)`).
    - Chemin pointant vers un fichier : `Dégradé` (`Le chemin configuré pointe vers un fichier au lieu d'un répertoire`).
    - Répertoire inaccessible : `Inaccessible` (`Erreur d'accès / refusé`).
    - Répertoire existant : `Opérationnel` avec mention `Accessible (N entrées stockées)`.
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
- **Tests unitaires et d'intégration ajoutés** :
  - `provider/internal/adapters/executors/artifact_verify_test.go` :
    - `TestArtifactVerifyValidateParameters` : 20 scénarios couvrant les paramètres valides, invalides, incomplets, traversées, champs inconnus et JSON corrompu.
    - `TestArtifactVerifyExecuteSuccess` : vérification réussie avec sortie structurée et preuve cryptographique.
    - `TestArtifactVerifyExecuteNotFound` : échec explicite pour artefact absent du catalogue.
    - `TestArtifactVerifyExecuteTamperedHashMismatch` : échec explicite en cas d'altération du contenu.
    - `TestArtifactVerifyExecuteUnauthorizedArtifactSpecification` : rejet des spécifications hors catalogue officiel.
    - `TestArtifactVerifyExecuteInvalidCatalogHashMetadata` : rejet des métadonnées de hash non conformes.
    - `TestArtifactVerifyExecuteContextCancellation` : respect du contexte d'annulation et des délais.
    - `TestArtifactVerifyRetryable` : non-rejouabilité des erreurs d'intégrité.
    - `TestArtifactVerifyIntegrationWithRealGeneratorAndExecutionService` : test d'intégration complet avec le véritable générateur `generation.Generate`, le vrai stockage de fichiers, le vrai `ExecutionService` et détection d'altération réelle sur le disque.
  - `provider/internal/delivery/httpapi/jobs_test.go` :
    - `TestExecutionAPIRunsArtifactVerifyTaskSuccessfully` : soumission HTTP POST `/api/v1/executions` authentifiée admin, attente du scheduler, et vérification de la persistance via GET `/api/v1/executions/:id`.
- **Vérifications et Tests exécutés** :
  - `make fmt-check` : code 0, 100% conforme `gofmt`.
  - `make vet` : code 0, aucune anomalie statique.
  - `make test-agent` : code 0, tests agent préservés.
  - `go test -count=1 ./provider/internal/adapters/executors` : 10 tests réussis (0 échec).
  - `go test -count=1 ./...` : code 0, 100% de la suite de tests du dépôt passe sans cache.
  - `make build` : code 0, compilation réussie de `bin/infraflow-provider` et `bin/infraflow-agent`.

## Remaining work

- Implement device-specific task executors (Cisco, FortiGate, MikroTik, Proxmox) only after defining their parameter schemas, privilege boundaries, and real lab verification evidence.
- Implement or explicitly defer a DNS service and other unimplemented bootstrap services.
- Run the final full test suite and binary builds in the separate final campaign. Docker and GNS3 validation remain separate deployment gates.

## Next implementation task

Define and authorize the first device-specific task executor adapter with strict privilege isolation, parameter validation, and acceptance evidence in a controlled lab environment. Do not register vendor executors without real lab verification.