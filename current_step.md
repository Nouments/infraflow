# INFRAFLOW — PLAN DE MATURITÉ GLOBALE EN 4 BLOCS

## OBJECTIF GÉNÉRAL

Tu interviens comme ingénieur logiciel senior, architecte réseau, ingénieur QA et responsable de fiabilité sur le dépôt existant `Nouments/infraflow`, branche `develop`.

L’objectif est d’amener InfraFlow à un niveau de maturité maximal et vérifiable avant son déploiement. Il ne s’agit pas simplement de faire passer quelques tests ni de corriger un seul exécuteur Cisco. Il faut examiner le produit dans son ensemble, identifier les lacunes réelles, améliorer le code, renforcer le générateur et vérifier le fonctionnement de bout en bout.

InfraFlow doit devenir une solution d’automatisation d’infrastructures réseau et système fondée sur une configuration déclarative YAML, avec un Provider, un Agent TUI, une API, un tableau de bord, un système de génération d’artefacts, un mécanisme d’exécution et un suivi fiable des opérations.

### Règles impératives

1. Inspecte le dépôt et le code réellement présents sur `develop` avant de modifier quoi que ce soit.
2. Ne suppose jamais qu’une fonctionnalité existe parce qu’elle est mentionnée dans la documentation.
3. Ne fabrique aucune donnée, sortie de commande, preuve de test, réussite d’exécution ou validation de laboratoire.
4. Ne remplace pas une vraie fonctionnalité par une simulation pour faire apparaître un résultat positif.
5. Ne réécris pas toute l’architecture si une correction ciblée suffit.
6. Préserve les fonctionnalités existantes, les contrats API, la compatibilité YAML et la sécurité.
7. Chaque correction importante doit être accompagnée de tests adaptés.
8. Distingue toujours les tests unitaires, les tests d’intégration, la validation des artefacts générés et les essais sur de véritables équipements.
9. Si un outil, une dépendance, un équipement ou un laboratoire n’est pas accessible, indique précisément le blocage. Ne prétends pas avoir effectué le test.
10. Ne présente jamais un artefact généré comme une infrastructure déployée.
11. Ne considère pas le travail terminé simplement parce que la compilation réussit.
12. Travaille uniquement sur le bloc demandé. Ne commence pas le bloc suivant sans validation explicite.

### État des fonctionnalités

Pour chaque fonctionnalité, utilise obligatoirement l’un de ces statuts :

- `IMPLEMENTED_AND_TESTED` : implémentée et testée avec des preuves adaptées.
- `IMPLEMENTED_NOT_FULLY_VERIFIED` : implémentée, mais les preuves restent insuffisantes.
- `PARTIAL` : une partie seulement du besoin est couverte.
- `BLOCKED` : la vérification ou l’implémentation est empêchée par un blocage identifié.
- `NOT_IMPLEMENTED` : fonctionnalité absente du code.

Ne donne pas le statut `IMPLEMENTED_AND_TESTED` sur la seule base d’une inspection visuelle du code.

---

# BLOC 1 — AUDIT GLOBAL, ARCHITECTURE ET CORRECTIONS CRITIQUES

## Objectif

Comprendre précisément le produit actuel, établir son état réel et éliminer les défauts critiques avant d’étendre les fonctionnalités.

## Étape 1 : inspection complète

Examine notamment, si ces éléments existent :

- `INFRAFLOW_SPEC.md`
- `CURRENT.md`
- `current_step.md`
- `README.md`
- `ARCHITECTURE.md`
- les fichiers YAML d’exemple et leurs schémas
- le parsing et la validation YAML
- la validation sémantique et le planificateur
- les générateurs Ansible, Terraform et bootstrap
- les manifestes, le catalogue et le stockage des artefacts
- les exécuteurs et le cycle de vie des tâches
- le scheduler, les tentatives, les annulations et la reprise
- les API HTTP et gRPC
- l’authentification, les sessions et les autorisations
- l’Agent TUI
- le dashboard web
- les journaux, les événements et les rapports
- les tests, scripts, Makefile, Dockerfiles et pipelines CI.

Inspecte également les dépendances, les points d’entrée, les interfaces entre modules et les chemins d’exécution réellement utilisés.

## Étape 2 : cartographie des fonctionnalités

Crée une matrice indiquant, pour chaque fonctionnalité :

- le besoin attendu ;
- le statut réel ;
- les fichiers et fonctions concernés ;
- les tests existants ;
- les preuves disponibles ;
- les défauts constatés ;
- le niveau de priorité ;
- la correction proposée.

Classe les défauts selon les priorités suivantes :

- **P0** : vulnérabilité, perte ou corruption de données, fausse réussite, mutation dangereuse ou fuite de secrets.
- **P1** : blocage du générateur, rupture du parcours principal ou intégration inutilisable.
- **P2** : incohérence fonctionnelle, manque de tests ou problème de fiabilité.
- **P3** : amélioration secondaire, ergonomie ou dette technique non bloquante.

## Étape 3 : corrections

Corrige les problèmes P0 et P1 confirmés dans le périmètre du bloc.

Vérifie notamment :

- gestion des erreurs et propagation des échecs ;
- validation des entrées et des chemins de fichiers ;
- absence de secrets dans les journaux ;
- cohérence des états et résultats persistés ;
- gestion des fichiers temporaires et des écritures partielles ;
- compatibilité des contrats entre modules ;
- absence de succès déclaré lorsqu’une opération a échoué ou n’a pas été vérifiée ;
- respect des limites de permissions et des règles d’authentification.

N’effectue aucune mutation réelle sur un équipement pendant ce bloc sans autorisation explicite et environnement de test confirmé.

## Étape 4 : tests et compte rendu

Exécute les tests ciblés pertinents et les contrôles statiques disponibles.

Présente :

1. la cartographie des fonctionnalités ;
2. les défauts P0/P1 identifiés ;
3. les corrections réellement appliquées ;
4. les fichiers modifiés ;
5. les commandes exécutées et leurs résultats ;
6. les problèmes encore ouverts ;
7. le commit créé, si les changements sont cohérents.

Mets à jour `CURRENT.md` et `current_step.md` uniquement avec des informations confirmées.

**Critère de sortie :** l’état réel du produit est documenté, les problèmes critiques confirmés sont corrigés et les preuves sont disponibles. Arrête-toi et attends la validation.

---

# BLOC 2 — GÉNÉRATEUR : FIABILITÉ, COHÉRENCE ET ARTEFACTS EXPLOITABLES

## Objectif

Faire du générateur le cœur fiable d’InfraFlow. Le système doit produire des artefacts cohérents, déterministes, contrôlables et réellement exploitables, sans prétendre effectuer des opérations qu’ils ne réalisent pas.

## Étape 1 : établir le contrat YAML

À partir du code réellement existant, documente le contrat YAML supporté :

- sites et infrastructures ;
- équipements ;
- constructeurs, modèles et rôles ;
- interfaces et adresses IP ;
- topologie et relations entre équipements ;
- paramètres de gestion ;
- fonctionnalités réseau ;
- services de bootstrap disponibles ;
- paramètres optionnels et valeurs par défaut.

N’ajoute pas arbitrairement de nouveaux champs. Un champ ne peut être considéré comme supporté que si son parsing, sa validation et son utilisation sont implémentés et testés.

Détecte explicitement :

- champs inconnus ;
- champs obligatoires manquants ;
- adresses ou préfixes invalides ;
- collisions d’adresses ;
- interfaces dupliquées ;
- références à des équipements inexistants ;
- combinaisons incompatibles ;
- constructeurs, modèles ou fonctionnalités non supportés ;
- secrets exposés ;
- chemins dangereux et valeurs malformées.

Les erreurs doivent désigner le champ concerné et expliquer comment corriger le YAML.

## Étape 2 : vérifier tout le pipeline de génération

Vérifie et renforce le parcours complet :

`YAML → parsing → validation → validation sémantique → planification → génération → manifeste → stockage/catalogue → vérification d’intégrité → consommation par l’Agent → résultat d’exécution`.

Chaque étape doit disposer d’un contrat clair, d’une gestion d’erreurs cohérente et de tests adaptés.

Assure-toi notamment que :

- une entrée invalide ne produit pas un artefact présenté comme valide ;
- les artefacts appartiennent à la bonne infrastructure et au bon site ;
- les dépendances entre équipements sont prises en compte lorsque nécessaire ;
- les fichiers générés sont complets et cohérents entre eux ;
- une génération échouée ne laisse pas de fichiers partiels présentés comme définitifs ;
- les anciennes sorties ne sont pas réutilisées silencieusement ;
- les écritures importantes sont atomiques lorsque c’est possible ;
- le manifeste correspond aux fichiers réellement présents ;
- les empreintes SHA-256 sont calculées et vérifiées correctement ;
- une modification ou une corruption est détectée ;
- une même entrée et les mêmes paramètres produisent des résultats reproductibles lorsque le générateur est censé être déterministe.

## Étape 3 : rendre Ansible réellement vérifiable

Examine le générateur Ansible pour chaque profil réellement annoncé, notamment Cisco IOS/IOS-XE, MikroTik RouterOS et Fortinet/FortiGate.

Pour chaque profil, vérifie :

- inventaire généré ;
- playbooks ;
- variables et paramètres requis ;
- collections et dépendances ;
- syntaxe YAML ;
- syntaxe Ansible ;
- modules utilisés et compatibilité avec le constructeur ;
- sélection des équipements et des groupes ;
- idempotence lorsque la fonctionnalité le permet ;
- mode de vérification ;
- conditions autorisant une mutation réelle ;
- gestion des erreurs et du résultat final.

N’annonce pas une prise en charge complète d’un constructeur si seul un squelette de configuration est généré.

Lorsque les outils sont disponibles, exécute les validations adaptées, par exemple `ansible-playbook --syntax-check` et les contrôles d’inventaire. Si les dépendances ou collections manquent, consigne le blocage exact.

Un mode manuel, un mode de vérification ou un manifeste `UNVERIFIED` doit rester clairement présenté comme tel. Ne transforme pas ces états en réussite d’application.

## Étape 4 : clarifier et fiabiliser Terraform

Inspecte les fichiers Terraform générés.

Détermine précisément s’ils servent à :

- décrire des données ;
- structurer des variables ;
- produire des informations de sortie ;
- configurer un fournisseur ;
- créer ou modifier de véritables ressources.

Si le générateur ne produit que des données et aucune ressource provisionnée, documente cette limite clairement. Ne prétends pas que Terraform déploie des équipements réseau si aucun fournisseur ni aucune ressource correspondante n’est implémenté.

Lorsque Terraform est disponible, utilise les contrôles adaptés, par exemple :

- `terraform fmt -check` ;
- `terraform init -backend=false` dans un environnement contrôlé ;
- `terraform validate`.

Ne télécharge pas de fournisseur arbitraire ni ne déclenche un déploiement réel sans nécessité et autorisation.

## Étape 5 : tester la qualité des artefacts

Ajoute des tests qui vérifient le contenu des fichiers générés, pas uniquement leur existence.

Couvre notamment :

- YAML valide et invalide ;
- infrastructure mono-site et multi-sites ;
- plusieurs équipements ;
- interfaces et adressage ;
- constructeur non supporté ;
- modèle incompatible ;
- champs inconnus ;
- chemins malveillants ;
- valeurs manquantes ;
- génération répétée ;
- manifeste incomplet ;
- empreinte incorrecte ;
- fichier altéré ;
- erreurs d’écriture ;
- échec partiel ;
- fuite de secrets ;
- cohérence entre inventaire, playbook, variables et manifeste ;
- syntaxe des artefacts lorsque les outils correspondants sont disponibles.

## Étape 6 : distinguer génération et déploiement

Le système doit différencier explicitement les états :

- configuration analysée ;
- plan produit ;
- artefacts générés ;
- artefacts vérifiés ;
- artefacts publiés ;
- tâche envoyée à l’Agent ;
- exécution démarrée ;
- exécution échouée ;
- exécution réussie ;
- résultat vérifié sur l’équipement.

N’utilise jamais un seul état `success` pour représenter toutes ces étapes.

**Critère de sortie :** le générateur est testé au niveau de son contenu, ses limites sont documentées, les artefacts sont cohérents et les validations disponibles ont été exécutées. Arrête-toi et attends la validation.

---

# BLOC 3 — INTÉGRATION COMPLÈTE PROVIDER, AGENT, API, TUI ET DASHBOARD

## Objectif

Vérifier que les composants fonctionnent ensemble avec de véritables états, des résultats persistés et une gestion fiable des erreurs. Le but est de supprimer les écarts entre ce que l’interface affiche et ce que le système a réellement exécuté.

## Étape 1 : cycle de vie des tâches

Inspecte l’enregistrement des exécuteurs, la planification et l’exécution.

Vérifie notamment :

- rejet explicite des types de tâches non supportés ;
- validation des paramètres avant exécution ;
- respect des dépendances ;
- prévention des exécutions concurrentes incompatibles ;
- gestion des délais d’attente ;
- annulation ;
- tentatives et reprises ;
- persistance des états et résultats ;
- récupération après redémarrage ;
- journal d’audit ;
- sortie standard et sortie d’erreur limitées ;
- distinction entre échec, annulation, expiration et réussite.

Les reprises automatiques doivent être adaptées à la nature de l’opération. Ne réessaie pas aveuglément une mutation réseau non idempotente.

Une tâche ne doit jamais être marquée comme réussie simplement parce qu’elle a été acceptée, planifiée ou envoyée à un Agent.

## Étape 2 : API et sécurité

Examine les routes réellement exposées, les contrats de requête et les réponses.

Teste notamment :

- authentification et expiration des sessions ;
- permissions et rôles ;
- requêtes malformées ;
- champs inconnus ;
- accès non autorisé ;
- opérations administratives ;
- annulation d’exécution ;
- rapports et audits ;
- gestion des erreurs ;
- absence de secrets dans les réponses et journaux ;
- cohérence des états persistés ;
- comportement lors d’une perte de connexion ou d’une erreur de stockage.

Les opérations dangereuses doivent être protégées par des autorisations explicites et des contrôles adaptés. Ne désactive pas la vérification des certificats ou des clés SSH pour simplifier les tests.

## Étape 3 : Agent TUI

Vérifie que le TUI utilise les véritables contrats et données du Provider.

Assure-toi qu’il distingue :

- Agent connecté ou déconnecté ;
- heartbeat reçu ou expiré ;
- artefact reçu ou absent ;
- artefact vérifié ou rejeté ;
- tâche en attente, en cours ou terminée ;
- exécution réussie, échouée ou annulée ;
- synchronisation locale et synchronisation distante ;
- erreurs de connexion et erreurs d’exécution.

Teste la reconnexion, les erreurs réseau, les données invalides, les résultats incomplets et les opérations indisponibles.

Aucune donnée fictive ne doit être présentée comme un état opérationnel réel.

## Étape 4 : dashboard et observabilité

Vérifie les pages, les tableaux, les détails d’exécution, les événements et les journaux.

Le dashboard doit présenter uniquement des données disponibles et clairement qualifier les informations non vérifiées.

Contrôle :

- cohérence entre les listes et les détails ;
- actualisation des états ;
- streaming SSE, si implémenté ;
- fermeture des connexions et nettoyage des ressources ;
- erreurs de chargement ;
- annulation d’une tâche ;
- filtres et pagination, s’ils existent ;
- affichage des logs et résultats ;
- masquage des secrets ;
- comportement après redémarrage ;
- état de santé des composants.

Ne crée pas de fausses données pour rendre le dashboard plus rempli. Si une information n’est pas encore disponible, affiche un état vide ou une limitation explicite.

## Étape 5 : scénario d’intégration

Construis et exécute un scénario reproductible utilisant les composants réellement disponibles :

1. charger un YAML valide ;
2. le valider ;
3. produire un plan ;
4. générer les artefacts ;
5. vérifier le manifeste et les empreintes ;
6. publier ou enregistrer les artefacts ;
7. les rendre accessibles à l’Agent ;
8. exécuter une tâche effectivement supportée ;
9. récupérer le résultat ;
10. vérifier sa persistance ;
11. consulter le résultat via l’API, le TUI et le dashboard lorsque ces chemins sont disponibles ;
12. vérifier les journaux et les erreurs.

Ajoute des scénarios négatifs : artefact altéré, tâche inconnue, Agent déconnecté, entrée invalide, erreur d’exécution et annulation.

Si le scénario complet n’est pas réalisable, indique la dernière étape validée et le blocage exact. Ne simule pas les étapes manquantes.

**Critère de sortie :** le parcours intégré est démontré avec des preuves vérifiables, ou les blocages restants sont précisément identifiés. Arrête-toi et attends la validation.

---

# BLOC 4 — VALIDATION RÉELLE, DOCUMENTATION, DÉMONSTRATION ET PRÉ-DÉPLOIEMENT

## Objectif

Préparer une démonstration fiable et évaluer honnêtement la maturité du produit avant tout déploiement.

## Étape 1 : validation technique générale

Depuis la racine du dépôt, vérifie les commandes réellement disponibles et exécute, lorsque cela correspond au projet :

```bash
make fmt-check
go test -count=1 ./provider/internal/adapters/executors
go test -count=1 ./provider/internal/application/...
go test -count=1 ./...
make vet
make build
git diff --check
git status --short
git diff --stat
```

Adapte les commandes uniquement si le dépôt confirme que leur nom ou leur périmètre diffère. Explique toute adaptation.

Ne déclare pas la suite complète réussie si une commande a échoué, n’a pas été exécutée ou a été ignorée. Consigne les erreurs et leurs causes.

Exécute aussi les validations Ansible et Terraform appropriées lorsque les outils et dépendances sont disponibles.

## Étape 2 : validation Cisco et laboratoire

Examine les capacités Cisco réellement implémentées.

Distingue au minimum :

- génération de configuration Cisco ;
- connexion SSH ;
- envoi de commandes ;
- lecture de la configuration de retour ;
- vérification des changements ;
- sauvegarde persistante de la configuration ;
- prise en charge des fonctions réseau avancées.

Ne considère pas ces capacités comme équivalentes.

Si un laboratoire GNS3 ou un véritable équipement Cisco est accessible et autorisé :

1. vérifie le modèle et la version réellement disponibles ;
2. documente l’adressage et les prérequis ;
3. vérifie la connectivité et l’authentification ;
4. exécute uniquement les tests autorisés ;
5. récupère les sorties et résultats réels ;
6. vérifie la configuration après application ;
7. documente précisément les changements et les limites.

N’effectue aucune modification sur un équipement de production. N’exécute pas de sauvegarde persistante telle que `write memory` sans autorisation explicite.

Si le laboratoire n’est pas accessible, prépare une procédure reproductible et indique `NON EXÉCUTÉ — LABORATOIRE INDISPONIBLE`. Ne transforme pas une simulation en preuve d’exécution réelle.

## Étape 3 : documentation et démonstration

Mets à jour les documents nécessaires à partir des fonctionnalités confirmées.

La documentation doit préciser :

- architecture actuelle ;
- prérequis ;
- installation et configuration ;
- format YAML supporté ;
- validation et planification ;
- génération des artefacts ;
- utilisation du Provider ;
- utilisation de l’Agent TUI ;
- API et dashboard ;
- exécution des tâches supportées ;
- limites connues ;
- sécurité et gestion des secrets ;
- procédures de test ;
- procédure de démonstration ;
- état réel de la validation Cisco.

Prépare une démonstration de bout en bout avec des données explicites et des résultats vérifiables. Si une étape dépend d’un équipement absent, présente-la comme non validée plutôt que de la maquiller.

## Étape 4 : évaluation finale de maturité

Établis un rapport final comportant :

1. les fonctionnalités et leur statut ;
2. les corrections apportées dans chaque bloc ;
3. les tests exécutés et leurs résultats ;
4. les artefacts générés et leurs validations ;
5. le scénario d’intégration réellement exécuté ;
6. l’état de la validation Cisco ;
7. les risques de sécurité ;
8. les défauts P0, P1, P2 et P3 encore ouverts ;
9. les éléments empêchant le déploiement ;
10. les prochaines actions classées par priorité.

Attribue un score de maturité sur 100 uniquement avec une grille explicite. Le score ne doit pas remplacer les preuves.

Une fonctionnalité critique non testée, un défaut P0 ouvert, une fausse déclaration de réussite ou un parcours essentiel non vérifié doit empêcher de qualifier le produit de prêt pour la production.

Utilise les niveaux suivants :

- **Code** : compilation, tests, sécurité et robustesse.
- **Générateur** : exactitude, cohérence, reproductibilité et validation des artefacts.
- **Intégration** : fonctionnement réel du Provider, de l’Agent, de l’API et des interfaces.
- **Laboratoire** : exécution vérifiée sur les équipements et plateformes ciblés.

Ne donne pas automatiquement 100/100. Le score doit refléter les preuves disponibles, pas l’effort fourni ni le nombre de fichiers modifiés.

## Règles de livraison

À la fin de chaque bloc :

- présente un résumé des modifications ;
- liste les fichiers modifiés ;
- indique les commandes et résultats réels ;
- expose les limites et les points non testés ;
- mets à jour `CURRENT.md` et `current_step.md` avec des informations vérifiées ;
- crée un commit cohérent si les changements sont testés et prêts ;
- fournis le SHA du commit ;
- attends une validation explicite avant de continuer.

Ne mélange pas des changements sans rapport dans un même commit. N’écrase pas les modifications préexistantes de l’utilisateur.

## Première action demandée

Commence uniquement par le **BLOC 1**. Inspecte l’état réel de la branche `develop`, établis la cartographie et corrige les défauts critiques confirmés. N’entame pas le BLOC 2 avant ma validation.

La priorité absolue est la fiabilité réelle du produit, en particulier du générateur et du parcours d’exécution. Le résultat attendu est une solution démontrable et vérifiable, pas simplement un dépôt qui compile.