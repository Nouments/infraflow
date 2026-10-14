# INFRAFLOW — MISSION DE FERMETURE DU BLOC 1
## Corrections finales, validation QA et preuves reproductibles

### CONTEXTE

Dépôt : `Nouments/infraflow`  
Branche obligatoire : `develop`  
Dernier commit connu : `7c796c011fdbe2725c15153d3f0828dd97253043`  
Fichier de suivi : `current_step.md`  
Rapport d'état : `CURRENT.md`

Tu es responsable de l'implémentation, des corrections et de la validation technique du Bloc 1 d'InfraFlow.

**Objectif : terminer réellement le Bloc 1, vérifier le dernier commit et fournir des preuves reproductibles permettant au PO/QA de prendre une décision.**

Le dernier commit a notamment introduit une limite commune de 32 Mio pour les artefacts, des contrôles supplémentaires dans le catalogue, le Provider et le transfert gRPC, ainsi que des tests associés.

Le rapport `CURRENT.md` doit être vérifié : il peut encore décrire l'audit précédent sans prendre en compte le dernier commit.

### RÈGLES ABSOLUES

1. Reste sur `develop`.
2. Inspecte le HEAD, le statut Git et les modifications locales avant toute action.
3. Lis `current_step.md`, `CURRENT.md`, `INFRAFLOW_SPEC.md` et les fichiers concernés.
4. Préserve toutes les modifications préexistantes de l'utilisateur.
5. N'utilise jamais `git reset --hard`, `git clean -fd` ou une commande destructive.
6. Ne réécris pas l'architecture sans nécessité démontrée.
7. Ne fabrique aucune donnée, preuve, sortie de commande ou réussite.
8. Ne confonds jamais génération, validation syntaxique, exécution réelle et vérification après application.
9. Ne lance aucune configuration sur un équipement réel sans autorisation explicite et cible de laboratoire confirmée.
10. Ne commence pas le Bloc 2.
11. N'effectue aucun push sans autorisation.
12. Travaille en quatre blocs successifs. Termine et documente chaque bloc avant de passer au suivant.

---

# BLOC 1 — AUDIT DU DERNIER COMMIT ET CORRECTIONS

## Objectif

Vérifier le dernier commit, confirmer que les changements sont corrects et corriger uniquement les défauts démontrés.

### 1.1. Vérification Git

Exécute et conserve les résultats de :

- `git branch --show-current`
- `git rev-parse HEAD`
- `git status --short --branch`
- `git log -5 --oneline`
- `git show --stat --oneline HEAD`
- `git diff --check`

Confirme que le commit `7c796c011fdbe2725c15153d3f0828dd97253043` est bien le commit courant ou explique précisément pourquoi il ne l'est pas.

Inspecte le diff du dernier commit et les éventuelles modifications locales. Ne suppose pas que le dépôt est propre.

### 1.2. Vérification de la limite des artefacts

Inspecte en priorité :

- `pkg/protocol/artifact.go`
- `internal/adapters/generation/catalog.go`
- `internal/adapters/generation/catalog_test.go`
- `provider/internal/adapters/filesystem/artifacts.go`
- `provider/internal/adapters/executors/artifact_verify.go`
- `provider/internal/delivery/grpcapi/server.go`
- `agent/internal/adapters/providergrpc/client.go`
- `agent/internal/delivery/artifacts/http.go`

Vérifie que `protocol.MaxArtifactBytes` est réellement appliqué à toutes les opérations pertinentes.

Teste notamment :

- un artefact valide sous la limite ;
- un artefact exactement à la limite, si ce test est réalisable sans allocation excessive ;
- un artefact dépassant la limite ;
- un fichier dont la taille change entre le contrôle initial et la lecture ;
- une erreur de lecture ou de vérification ;
- une transmission gRPC interrompue ou dépassant la limite ;
- la cohérence entre la taille acceptée par le catalogue, le Provider et l'Agent.

Vérifie également les risques de lecture excessive, d'allocation mémoire injustifiée, de troncature silencieuse et de divergence entre la taille annoncée et le contenu réellement transmis.

Ne prétends pas éliminer les courses entre fichiers si les mécanismes de lecture et de vérification ne le démontrent pas.

### 1.3. Corrections

Pour chaque défaut confirmé :

1. Reproduis le problème.
2. Identifie sa cause.
3. Ajoute un test de régression.
4. Corrige le code minimalement.
5. Relance les tests ciblés.
6. Documente le résultat réel.

Si aucun défaut n'est trouvé, ne modifie pas le code artificiellement.

**Critère de sortie :** les changements du dernier commit sont examinés et les défauts identifiés sont corrigés ou explicitement documentés.

---

# BLOC 2 — TESTS COMPLETS ET VALIDATION DES OUTILS

## Objectif

Vérifier que les corrections et le reste du dépôt restent cohérents.

### 2.1. Suite Go

Exécute :

```bash
go test -count=1 ./...
make fmt-check
make vet
make build
git diff --check
```

Conserve les codes de sortie et les erreurs réelles.

Si une commande échoue, diagnostique la cause avant de continuer. Ne modifie pas les tests uniquement pour obtenir un résultat positif.

Exécute également les tests ciblés des packages modifiés et, si pertinent, les tests de concurrence ou de course sur les composants concernés.

### 2.2. Ansible

Vérifie la disponibilité réelle de :

```bash
ansible-playbook --version
ansible-galaxy collection list
```

Inspecte les profils générés par InfraFlow.

Lorsque les dépendances sont disponibles :

- installe uniquement les collections nécessaires dans l'environnement de validation autorisé ;
- génère les artefacts à partir d'entrées explicites ;
- exécute `ansible-playbook --syntax-check` sur chaque playbook applicable ;
- vérifie les inventaires, variables, collections et méthodes de connexion ;
- vérifie que les opérations de modification sont protégées par les autorisations prévues.

Ne confonds pas un YAML valide avec un playbook opérationnel.

Ne lance aucune modification réelle d'équipement pendant ce bloc.

Si Ansible ou une collection manque, indique `BLOCKED` avec la commande et l'erreur exacte.

### 2.3. Terraform

Vérifie :

```bash
terraform version
```

Inspecte les fichiers réellement produits. Si Terraform est disponible et que les fichiers le permettent, exécute dans un répertoire temporaire isolé :

```bash
terraform fmt -check
terraform init -backend=false
terraform validate
```

N'exécute jamais `terraform apply`.

Précise si InfraFlow produit réellement des ressources de provisionnement ou uniquement des fichiers de données.

Si Terraform est absent, indique `BLOCKED`. Ne simule pas sa validation.

**Critère de sortie :** les contrôles locaux sont exécutés et les validations externes sont classées sans ambiguïté.

---

# BLOC 3 — VALIDATION CISCO/GNS3 ET ÉTAT RÉEL DES FONCTIONNALITÉS

## Objectif

Établir ce qui fonctionne réellement, sans confondre tests automatisés et déploiement opérationnel.

### 3.1. Cisco IOS dans GNS3

Vérifie d'abord que le laboratoire est accessible et que la cible est autorisée.

Contrôle :

- la plateforme et la version IOS exactes ;
- la connectivité depuis l'environnement réel d'exécution ;
- la disponibilité de SSH ;
- la validation des clés d'hôte ;
- les identifiants, sans les afficher dans les journaux ;
- la compatibilité de l'exécuteur avec l'équipement.

Si une cible de laboratoire est accessible et qu'une autorisation explicite existe, réalise une modification minimale, réversible et sans risque pour la gestion du routeur.

Avant application, relève l'état initial et vérifie le changement prévu.

Après application, reconnecte-toi et relis la configuration effective pour confirmer précisément le résultat.

Ne considère pas une connexion SSH réussie comme une preuve de configuration correcte.

Ne sauvegarde pas la configuration de manière permanente sans nécessité et autorisation.

Si la cible ou les accès manquent, n'invente rien : indique `BLOCKED` ou `NOT RUN`, avec le prérequis nécessaire.

### 3.2. Vérification fonctionnelle

Inspecte les fonctionnalités existantes d'InfraFlow et distingue clairement :

- parsing et validation YAML ;
- planification des opérations ;
- génération des artefacts ;
- exécution réelle ;
- vérification après exécution ;
- TUI ;
- dashboard Web ;
- journalisation et rapports ;
- gestion des erreurs et reprise.

Pour chaque fonctionnalité, précise ce qui est implémenté, testé automatiquement et démontré sur un environnement réel.

N'ajoute aucune donnée fictive à la TUI ou au dashboard.

Ne présente pas un playbook de diagnostic comme une configuration réseau complète. Ne présente pas des données Terraform sans ressources comme un provisionnement effectif.

### 3.3. Sécurité

Vérifie au minimum :

- authentification et autorisation ;
- protection des secrets ;
- validation des entrées ;
- intégrité des artefacts ;
- contrôle des chemins ;
- propagation des erreurs ;
- distinction entre échec, blocage et succès ;
- cohérence des états entre Agent, Provider, API, TUI et Web.

Ne prétends pas avoir réalisé un audit de sécurité exhaustif.

**Critère de sortie :** les capacités démontrées et les limites restantes sont identifiées avec des preuves vérifiables.

---

# BLOC 4 — DOCUMENTATION, PREUVES ET DÉCISION FINALE

## Objectif

Synchroniser la documentation avec le code réellement vérifié et préparer une décision PO/QA.

### 4.1. Mise à jour documentaire

Mets à jour `CURRENT.md` et `current_step.md` uniquement après les validations précédentes.

Les fichiers doivent refléter :

- le dernier commit réellement inspecté ;
- les modifications effectuées pendant la mission ;
- les tests exécutés ;
- les validations réussies ;
- les validations échouées ou bloquées ;
- les limitations fonctionnelles restantes ;
- les prérequis nécessaires à la suite.

Ne remplace pas le contenu utile des consignes par un résumé incomplet. Conserve les critères d'acceptation et les règles de sécurité nécessaires à la reprise du travail.

### 4.2. Tableau des preuves

Pour chaque contrôle, consigne :

| Champ | Contenu requis |
|---|---|
| Contrôle | Identifiant et objectif |
| Environnement | Versions et prérequis |
| Commande | Commande réellement exécutée |
| Code de sortie | Valeur exacte |
| Statut | `PASS`, `FAIL`, `BLOCKED` ou `NOT RUN` |
| Preuve | Sortie pertinente ou test associé |
| Limite | Ce qui n'est pas démontré |

Sépare les preuves obtenues pendant cette mission des résultats historiques.

### 4.3. Contrôle final Git

Exécute :

```bash
git status --short --branch
git diff --check
git diff --stat
git diff
```

Inspecte les fichiers modifiés. Vérifie que les changements préexistants de l'utilisateur sont préservés et qu'aucun secret ou artefact temporaire n'a été ajouté par erreur.

Ne committe que si les consignes du dépôt et les autorisations le permettent. Ne pousse aucun changement sans autorisation.

### 4.4. Critères d'acceptation

Le Bloc 1 peut être proposé à l'approbation uniquement si :

- [ ] Le dernier commit et le diff ont été inspectés.
- [ ] Les tests de régression nécessaires sont présents.
- [ ] La suite Go pertinente réussit.
- [ ] Formatage, vet et build sont validés.
- [ ] La limite des artefacts est cohérente dans tous les chemins concernés.
- [ ] Les erreurs de lecture, d'intégrité et de transfert sont propagées correctement.
- [ ] Les validations Ansible et Terraform sont exécutées ou leur blocage est prouvé.
- [ ] Le test Cisco réel est réussi si cette preuve est requise ; sinon, son absence est explicitement signalée.
- [ ] `CURRENT.md` et `current_step.md` correspondent aux preuves.
- [ ] Aucun résultat non exécuté n'est présenté comme réussi.
- [ ] Les modifications locales préexistantes sont préservées.

Un contrôle bloqué ne devient jamais un succès parce qu'il est documenté.

## FORMAT DE RÉPONSE FINAL OBLIGATOIRE

Réponds uniquement avec les sections suivantes :

### A. Résumé
État réel du Bloc 1 et corrections effectuées.

### B. Preuves de tests
Commandes, codes de sortie, statuts et résultats.

### C. Validations externes
Ansible, Terraform et Cisco/GNS3.

### D. Fichiers modifiés
Liste précise et raison de chaque modification.

### E. Limitations restantes
Uniquement les problèmes réels et les preuves manquantes.

### F. Git
Branche, HEAD, statut, commit éventuel et préservation des modifications existantes.

### G. Décision PO/QA
Choisis exactement un statut :

- `BLOC 1 — PRÊT POUR APPROBATION PO/QA`
- `BLOC 1 — INCOMPLET : PREUVES MANQUANTES`
- `BLOC 1 — ÉCHEC : CORRECTIONS NÉCESSAIRES`

**Ne commence pas le Bloc 2 du projet InfraFlow. Attends l'approbation explicite du PO/QA après remise de ce bilan.**

Priorité absolue : fiabilité démontrée, sécurité et preuves reproductibles. Aucun objectif de score ne justifie de fabriquer un résultat.