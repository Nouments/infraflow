# MISSION FINALE — INFRAFLOW / FERMETURE STRICTE DU BLOC 1

## 1. Rôle et objectif

Tu es responsable de l'implémentation, de la correction et de la validation QA du dépôt `Nouments/infraflow`, branche `develop`.

**Objectif : terminer réellement le Bloc 1 et produire les preuves vérifiables nécessaires à sa validation PO/QA.**

Le Bloc 1 ne sera considéré comme terminé que si :
- le code et les tests sont cohérents ;
- les contrôles de sécurité et d'intégrité fonctionnent ;
- les artefacts générés sont validés avec les outils réels disponibles ;
- les limites fonctionnelles sont explicitement documentées ;
- les validations impossibles à exécuter sont déclarées comme telles ;
- `CURRENT.md` et `current_step.md` reflètent exactement l'état constaté.

Ne confonds jamais :
- code implémenté et code testé ;
- test unitaire et test d'intégration ;
- génération de configuration et application sur un équipement ;
- commande envoyée et configuration vérifiée après application ;
- validation documentée et validation reproduite ;
- absence d'erreur dans les tests et preuve de fonctionnement en production.

**Interdiction absolue :** inventer des résultats, simuler des équipements, fabriquer des journaux de succès, remplacer un test manquant par un test fictif ou déclarer le Bloc 1 terminé sans preuves.

## 2. Règles de travail

1. Reste sur `develop`. Vérifie le dépôt, le commit HEAD, le statut Git et les modifications existantes avant toute modification.
2. Lis `CURRENT.md`, `current_step.md`, `INFRAFLOW_SPEC.md`, ainsi que les fichiers de génération, d'intégrité, d'exécution et leurs tests.
3. Pars de l'état réel du dépôt. Ne réécris pas l'architecture pour des raisons esthétiques.
4. Conserve les modifications préexistantes de l'utilisateur. Ne fais aucun `git reset --hard`, `git clean -fd`, écrasement ou restauration destructive.
5. Corrige uniquement les problèmes démontrés par une inspection ou un test reproductible.
6. Après chaque correction, exécute les tests concernés, puis la suite pertinente.
7. N'ajoute aucune dépendance lourde si un outil existant suffit.
8. N'implémente pas de nouvelle fonctionnalité hors périmètre pour gonfler artificiellement le résultat.
9. Si une commande échoue, conserve son véritable code de sortie et son message d'erreur. Diagnostique et corrige si cela relève du dépôt.
10. Si un équipement, une collection, un accès ou un outil manque, indique exactement lequel. Ne contourne pas le problème en produisant une fausse preuve.

## 3. Axe A — Vérification du code et des tests

Inspecte les changements récents, notamment ceux concernant :
- `internal/adapters/generation/catalog.go`
- `internal/adapters/generation/vendor_ansible.go`
- `provider/internal/application/plan_generation.go`
- les adaptateurs de fichiers et d'exécution ;
- les tests du générateur, du catalogue et du cycle de vie des tâches.

Vérifie au minimum les points suivants :

### Catalogue et intégrité des artefacts
- Le manifeste est décodé strictement et les données supplémentaires invalides sont rejetées.
- Les chemins absolus, chemins traversants, liens symboliques et fichiers non réguliers ne permettent pas de sortir du répertoire autorisé.
- Les chemins dupliqués sont rejetés.
- Chaque artefact est comparé au SHA-256 attendu.
- Un fichier modifié après la génération est refusé à la lecture ou à la vérification.
- Une erreur d'intégrité ne peut jamais devenir un succès.
- Les contrôles sont cohérents entre catalogue, lecture des fichiers, téléchargement API et exécution.

Ajoute ou corrige les tests ciblés pour chaque faille réellement constatée. N'affirme pas que les liens symboliques ou les courses entre fichiers sont intégralement sécurisés sans vérifier les chemins de code concernés.

### Génération
- La sortie est déterministe pour une même entrée.
- Les entrées utilisateur ne permettent pas d'injecter des chemins ou du contenu non prévu.
- Les configurations non prises en charge sont refusées explicitement.
- Le générateur générique Ansible ne doit pas être présenté comme un playbook de configuration réseau opérationnel s'il ne produit que des données ou des tâches de diagnostic.
- Le générateur fournisseur doit rester distinct et ne doit pas inventer d'adresses, de routes ou de paramètres absents de l'entrée.
- Terraform ne doit pas être présenté comme provisionnant des équipements si aucun provider et aucune ressource de provisionnement ne sont générés.

### Planification et exécution
- La génération seule reste identifiable comme une génération.
- `NOT_REQUESTED`, `NOT_PERFORMED`, `UNVERIFIED` ou leurs équivalents ne sont jamais transformés en succès.
- Une tâche n'est pas marquée réussie avant que son exécuteur ait terminé et fourni le résultat attendu.
- Les erreurs de l'exécuteur, les délais d'attente et les échecs de vérification sont propagés correctement.
- Les nouvelles tentatives ne doivent pas être ajoutées à une opération réseau non idempotente sans justification.
- La vérification après configuration Cisco doit effectivement relire la configuration de l'équipement, et pas simplement confirmer que la connexion SSH a réussi.

Exécute les tests ciblés, puis les commandes Go du dépôt. Utilise les commandes existantes documentées par le projet, notamment si elles sont toujours pertinentes :

- `go test -count=1 ./...`
- `make fmt-check`
- `make vet`
- `make build`
- `git diff --check`

Si une commande échoue, analyse la cause et corrige le problème si nécessaire. Ne modifie pas les tests simplement pour faire passer la suite.

## 4. Axe B — Validation réelle d'Ansible

Inspecte les profils générés et identifie les collections réellement requises pour Cisco IOS/IOS-XE, MikroTik et Fortinet. Ne suppose pas qu'une collection est installée parce qu'elle est déclarée dans `requirements.yml`.

1. Génère les artefacts avec des entrées de test explicites et reproductibles.
2. Installe les collections nécessaires uniquement dans l'environnement de validation autorisé.
3. Vérifie les versions et la présence des outils.
4. Exécute `ansible-playbook --syntax-check` sur chaque playbook généré applicable.
5. Si l'inventaire et les paramètres le permettent, exécute une vérification non destructive en mode check. Documente ses limites : le mode check ne prouve pas qu'une configuration peut être appliquée réellement.
6. Vérifie la cohérence entre playbook, inventaire, collections, variables, méthodes de connexion et paramètres TLS.
7. Confirme qu'aucune opération de modification d'équipement n'est exécutée par défaut.
8. Confirme que le mode de modification nécessite bien le mécanisme d'autorisation prévu par le projet, dont `INFRAFLOW_APPLY=true` si cette règle est toujours présente dans le code.
9. Vérifie que les profils refusent toujours les intentions non implémentées, notamment celles que le code ne sait pas encore générer correctement.

N'applique aucune configuration réelle avec Ansible pendant cette étape sans autorisation explicite et sans cible de laboratoire désignée.

**Preuves attendues :** commandes exactes, versions, profils testés, codes de sortie, erreurs éventuelles et conclusion pour chaque playbook.

Si Ansible ou une collection manque, distingue clairement :
- validation syntaxique exécutée et réussie ;
- validation bloquée par une dépendance absente ;
- exécution réelle sur équipement non effectuée.

## 5. Axe C — Validation Terraform

1. Identifie exactement les fichiers Terraform réellement produits par InfraFlow.
2. Vérifie si le générateur produit uniquement des données ou un véritable plan de provisionnement.
3. Vérifie la syntaxe et le format avec les commandes Terraform adaptées, notamment `terraform fmt -check` lorsque pertinent.
4. Lance `terraform init -backend=false` puis `terraform validate` dans un répertoire temporaire isolé si les fichiers générés permettent cette validation.
5. Ne supprime pas les fichiers de travail de l'utilisateur.
6. N'exécute jamais `terraform apply` sur une infrastructure réelle pour satisfaire cette mission.
7. Si le code généré ne déclare aucun provider ni aucune ressource de provisionnement, documente précisément ce que Terraform valide et ce qu'il ne fait pas.

Ne crée pas artificiellement un provider ou des ressources simplement pour afficher un résultat positif. Corrige uniquement les défauts du périmètre actuel.

**Preuves attendues :** fichiers validés, version Terraform, commandes, codes de sortie et conclusion. Si Terraform est absent, la validation est bloquée ; elle n'est pas réussie.

## 6. Axe D — Test Cisco IOS réel dans GNS3

Ce test est prioritaire, mais il ne doit être effectué que si le laboratoire et la cible sont réellement accessibles et autorisés.

### D.1. Précontrôles

- Vérifie l'accès à GNS3 et l'état de l'équipement cible.
- Identifie le type exact d'image IOS et les fonctionnalités disponibles.
- Vérifie la connectivité IP et SSH depuis l'environnement qui exécute réellement l'agent.
- Vérifie les identifiants sans les imprimer dans les journaux.
- Vérifie les clés d'hôte SSH. Ne désactive pas la vérification de clé et n'utilise pas de contournement permanent.
- Vérifie que le pilote Ansible ou l'exécuteur utilisé est compatible avec l'image.
- Vérifie que le changement proposé est compatible avec les commandes réellement disponibles sur l'image.

Ne suppose pas qu'un routeur Cisco simulé prend en charge ZTP, IOS-XE ou toutes les commandes d'une autre plateforme.

### D.2. Exécution contrôlée

Utilise une cible de laboratoire autorisée et une modification minimale, réversible et sans risque pour la connectivité de gestion.

Avant application :
- relève le hostname et la configuration pertinente ;
- conserve un état initial exploitable ;
- affiche le plan et les changements prévus ;
- vérifie qu'aucune adresse de gestion ou configuration critique ne sera écrasée.

Applique uniquement le changement autorisé. Ne lance pas de sauvegarde permanente de configuration sans nécessité et autorisation.

### D.3. Vérification après application

Après exécution :
- récupère le résultat réel de l'exécuteur ;
- reconnecte-toi à l'équipement ;
- relis la configuration effective ;
- vérifie précisément les lignes ou paramètres attendus ;
- compare l'état observé à l'état attendu ;
- enregistre les erreurs éventuelles et l'état final.

Une connexion réussie, une commande envoyée ou un code de sortie nul ne suffit pas à prouver que la configuration est correcte.

**Critère de réussite :** la modification autorisée est appliquée, puis confirmée par une lecture réelle de la configuration du Cisco.

Si GNS3 ou la cible est indisponible, n'invente pas de résultat et ne remplace pas ce test par un mock. Documente :
- la commande exécutée ;
- le blocage précis ;
- les preuves disponibles ;
- les prérequis nécessaires à la reprise.

Dans ce cas, le test reste `NON EXÉCUTÉ` ou `BLOQUÉ`, et le Bloc 1 ne peut pas être déclaré intégralement validé.

## 7. Axe E — Sécurité et cohérence des preuves

Inspecte les chemins d'entrée, de génération, de stockage, de téléchargement et d'exécution.

Vérifie notamment :
- authentification et autorisation des opérations sensibles ;
- gestion des secrets et absence de secrets dans les logs ;
- validation des entrées ;
- contrôle des chemins et des sommes de contrôle ;
- limites de taille et de lecture ;
- erreurs explicites ;
- absence de succès fabriqués ;
- cohérence entre état du backend, rapport d'exécution, TUI et API.

Ne prétends pas avoir réalisé un audit de sécurité exhaustif. Signale les contrôles vérifiés, les risques non testés et les limites restantes.

## 8. Axe F — Rapport et documentation

Mets à jour `CURRENT.md` et `current_step.md` à partir des résultats effectivement obtenus.

Pour chaque validation, consigne :
- identifiant du test ;
- objectif ;
- environnement et versions ;
- commande exacte ;
- résultat réel et code de sortie ;
- preuve ou extrait de sortie pertinent, sans secret ;
- statut parmi `PASS`, `FAIL`, `BLOCKED`, `NOT RUN` ;
- limitations et action restante.

Sépare les résultats reproduits pendant cette mission des résultats historiques rapportés par d'autres personnes ou par d'anciens documents.

Le statut final doit préciser :
- ce qui est réellement implémenté ;
- ce qui est couvert par des tests automatisés ;
- ce qui a été validé par les outils réels ;
- ce qui a été testé sur un équipement réel ou simulé ;
- ce qui reste hors périmètre.

N'ajoute pas de données fictives à la TUI ou au dashboard. Si les composants ne présentent pas encore correctement les états `UNVERIFIED`, `NOT PERFORMED`, `FAILED` ou `BLOCKED`, corrige uniquement les défauts directement liés au Bloc 1.

## 9. Axe G — Exécution finale et intégrité Git

À la fin :
1. Réexécute les tests ciblés après correction.
2. Réexécute les vérifications Go pertinentes.
3. Vérifie les playbooks Ansible et Terraform lorsque les dépendances sont disponibles.
4. Vérifie `git diff --check`.
5. Inspecte `git diff` et `git status`.
6. Confirme que les changements préexistants de l'utilisateur sont préservés.
7. Vérifie que la documentation ne prétend pas que les tests bloqués ont réussi.
8. N'ajoute aucune preuve générée artificiellement.

Ne committe que les modifications appartenant clairement à cette mission et seulement si cela respecte les consignes du dépôt. Ne pousse pas de changements sans l'autorisation requise. Si un commit est créé, indique son hash et les fichiers inclus.

## 10. Critères d'acceptation du Bloc 1

Le Bloc 1 peut être proposé à l'approbation PO/QA uniquement si :

- [ ] Les tests Go pertinents sont exécutés et réussissent.
- [ ] Le formatage, le vet et le build sont validés.
- [ ] Les contrôles d'intégrité des artefacts sont testés.
- [ ] Les intentions non prises en charge sont rejetées explicitement.
- [ ] Les playbooks Ansible sont syntaxiquement validés, ou leur blocage est démontré et documenté.
- [ ] Les fichiers Terraform sont validés, ou leur blocage est démontré et documenté.
- [ ] Le test Cisco réel avec lecture de configuration après application est réussi, si le Bloc 1 exige cette preuve.
- [ ] Aucun résultat non exécuté n'est présenté comme réussi.
- [ ] `CURRENT.md` et `current_step.md` sont cohérents avec les preuves.
- [ ] Le diff est propre et les changements préexistants sont préservés.
- [ ] Les limitations fonctionnelles restantes sont explicitement recensées.

**Important :** les éléments bloqués ou non exécutés ne deviennent pas des réussites par simple documentation. Si un prérequis externe empêche une validation, fournis un diagnostic reproductible et laisse le statut incomplet.

## 11. Format de réponse obligatoire

À la fin, réponds exclusivement avec ce bilan structuré :

### A. Résumé
- État réel du Bloc 1.
- Défauts corrigés.
- Résultat final et raisons.

### B. Tableau de preuves
Pour chaque test : nom, commande, code de sortie, statut et preuve.

### C. Tests externes
- Ansible et collections.
- Terraform.
- GNS3/Cisco IOS.
- Pour chacun : `PASS`, `FAIL`, `BLOCKED` ou `NOT RUN`, sans ambiguïté.

### D. Fichiers modifiés
Liste les fichiers et explique brièvement chaque changement.

### E. Limitations restantes
Liste uniquement les problèmes réels, les dépendances absentes et les validations non effectuées.

### F. Git
- Branche et HEAD.
- `git status`.
- Commit éventuel.
- Confirmation de préservation des modifications préexistantes.

### G. Décision proposée
Choisis exactement l'un des statuts :
- `BLOC 1 — PRÊT POUR APPROBATION PO/QA`
- `BLOC 1 — INCOMPLET : PREUVES MANQUANTES`
- `BLOC 1 — ÉCHEC : CORRECTIONS NÉCESSAIRES`

Ne commence pas le Bloc 2. Attends l'approbation explicite du PO/QA.

**Règle finale : la priorité est la fiabilité démontrée, pas l'apparence d'un score de 100 %.**