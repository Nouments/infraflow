# InfraFlow — Agent Task 02 : Chaîne réelle Plan → Génération → Exécution contrôlée

## 1. Contexte

Dépôt : `Nouments/infraflow`
Branche : `develop`

InfraFlow est un outil d’orchestration d’infrastructures réseau et système, avec backend Web et agent TUI.

Le projet doit produire des configurations réelles à partir d’une infrastructure déclarée, conserver les preuves d’exécution et distinguer strictement l’état désiré de l’état réellement appliqué.

Architecture cible :

`Infrastructure YAML → Validation → Plan → Génération → Exécution contrôlée → Résultat → Vérification`

Ne réécris pas le projet. Commence par examiner le code existant, ses interfaces, ses tests et le dernier état de `develop`. Réutilise les composants fonctionnels.

## 2. Objectifs de cette tâche

1. Vérifier le lien réel entre le modèle d’infrastructure et le planner.
2. Relier la génération existante au plan, sans produire artificiellement des configurations.
3. Garantir que chaque artefact généré est identifié, traçable et associé à son équipement.
4. Encadrer l’exécution des artefacts existants avec des contrôles de sécurité.
5. Enregistrer le résultat de chaque étape sans confondre génération, exécution et vérification.
6. Ajouter des tests unitaires et d’intégration déterministes.
7. Exposer des résultats exploitables par le backend Web et le TUI, en réutilisant les services existants si possible.

## 3. Étape A — Audit avant modification

Inspecter au minimum :

* `internal/application/planner/`
* `internal/domain/plan.go`
* `internal/domain/model.go`
* `internal/adapters/generation/`
* `internal/application/reconcile/`
* les packages d’exécution existants ;
* les handlers et services backend ;
* le TUI ;
* les tests et la configuration CI.

Rechercher les appels réels entre les composants, les interfaces déjà définies, les fonctions non utilisées, les TODO et les comportements simulés.

Ne considère pas qu’une fonctionnalité est opérationnelle uniquement parce qu’une structure ou une interface existe.

Avant de coder, fournir un résumé concis des composants réutilisables et des lacunes constatées.

## 4. Étape B — Génération liée au plan

À partir d’une infrastructure valide :

1. Construire le plan avec le planner existant.
2. Sélectionner uniquement les tâches compatibles avec un générateur réellement disponible.
3. Générer les artefacts adaptés au fournisseur, à la famille et à la méthode de configuration.
4. Associer chaque artefact à son site, son équipement, sa tâche et sa version de modèle.
5. Enregistrer son chemin, son format, son empreinte SHA-256 et son statut.
6. Retourner une erreur explicite si le générateur nécessaire n’existe pas ou si la configuration est invalide.

Ne génère pas de fichier de configuration vide ou fictif pour simuler une réussite.

Ne marque pas une capacité `LAB-VERIFIED` sans preuve issue d’un test réel. Une méthode reconnue ou un profil fournisseur ne constitue pas une preuve d’exécution.

Si un générateur existant ne prend pas en charge une tâche, retourne un résultat `UNSUPPORTED` ou `UNVERIFIED` selon le contrat actuel du projet. Ne prétends pas que la tâche est générée.

## 5. Étape C — Exécution contrôlée

Examiner d’abord si un moteur d’exécution est déjà implémenté.

S’il existe, réutiliser ses interfaces et renforcer les contrôles nécessaires.

S’il n’existe pas, implémenter uniquement la plus petite interface d’exécution cohérente avec l’architecture actuelle. Ne crée pas un moteur générique complet dans cette tâche.

Exigences :

* Mode vérification ou simulation clairement identifié comme tel.
* Exécution réelle uniquement sur demande explicite et avec les garde-fous déjà prévus.
* Pour les générateurs Ansible, respecter le mode check et le mécanisme `INFRAFLOW_APPLY=true` existant.
* Aucun lancement implicite de commande privilégiée.
* Pas de shell construit par concaténation de chaînes avec des données utilisateur.
* Arguments transmis de manière sûre au processus.
* Timeout, capture du code de retour, stdout et stderr avec limites raisonnables.
* Annulation et erreurs propagées correctement.
* Aucun secret écrit dans les journaux.
* Aucun succès déclaré si le processus échoue ou si son résultat n’a pas été obtenu.

Si une fonctionnalité d’exécution réelle n’existe pas encore, documenter cette limite plutôt que de fabriquer une réussite.

## 6. Étape D — Machine d’états et preuves

Respecter les distinctions suivantes :

* `PLANNED` : le plan existe.
* `GENERATED` : les artefacts ont réellement été générés.
* `EXECUTED` : une tentative d’exécution réelle a été lancée et son résultat enregistré.
* `VERIFIED` : un contrôle de vérification indépendant a réussi.
* `OBSERVED` : un état a été constaté à partir d’une source réelle.
* `LAB-TESTED` : un test a effectivement été réalisé sur le laboratoire.

Une génération réussie ne signifie pas que l’équipement a été configuré.

Un code de retour nul ne suffit pas, à lui seul, à garantir que l’état réseau souhaité est appliqué.

Un mode check, dry-run ou simulation ne doit jamais marquer une tâche `EXECUTED`.

Chaque tentative doit fournir au minimum :

* un identifiant ;
* la tâche et l’équipement concernés ;
* le type d’exécution ;
* l’heure de début et de fin si disponibles ;
* le résultat et le code de retour si disponibles ;
* les références vers les artefacts ;
* les erreurs expurgées de secrets ;
* les preuves de vérification, lorsqu’elles existent.

Ne remplace pas silencieusement l’état désiré par un résultat d’exécution. Ne marque pas `VERIFIED` sans vérification indépendante réussie.

## 7. Étape E — Backend et TUI

Vérifier les endpoints et commandes déjà présents.

Si l’architecture le permet, connecter le backend et le TUI aux services réels du plan, de la génération et de l’exécution.

Ils doivent afficher les états provenant du backend, et non des valeurs écrites en dur.

Les interfaces doivent distinguer clairement :

* plan disponible ;
* génération réussie ou échouée ;
* exécution non demandée, en cours, réussie ou échouée ;
* vérification réussie, échouée ou non effectuée ;
* capacité non vérifiée ou non prise en charge.

Ne refais pas le design complet du dashboard. Ne crée pas de nouvelles pages si les vues existantes peuvent être adaptées proprement.

Si la connexion complète nécessite une étape ultérieure, documenter précisément ce qui reste à connecter.

## 8. Tests obligatoires

Ajouter des tests déterministes couvrant au minimum :

1. Plan valide avec un équipement et des interfaces.
2. Génération réelle d’un artefact par un générateur pris en charge.
3. Équipement sans générateur compatible.
4. Configuration invalide.
5. Artefact associé au bon équipement et à la bonne tâche.
6. Empreinte SHA-256 stable pour un contenu identique.
7. Échec du générateur.
8. Mode simulation sans exécution réelle.
9. Exécution réelle en test contrôlé, sans équipement réseau obligatoire.
10. Échec du processus et code de retour non nul.
11. Interdiction de passer à `VERIFIED` sans preuve de vérification.
12. Absence de fuite de secrets dans les logs.
13. Backend/TUI utilisant les résultats réels lorsqu’un branchement est implémenté.

Utiliser des faux processus ou des doubles de test uniquement pour tester les frontières et les erreurs. Les résultats de ces tests doivent être identifiés comme tests, jamais comme preuves d’une configuration réseau réelle.

## 9. Contraintes

* Pas de réécriture globale.
* Pas de nouvelle dépendance lourde sans justification.
* Pas de données fictives dans les parcours présentés comme réels.
* Pas d’accès à des équipements réels pendant les tests automatisés.
* Pas de nouvelles fonctions NAT, DHCP, OSPF, ZTP ou PXE dans cette tâche, sauf si elles sont indispensables à la correction d’un comportement déjà implémenté.
* Pas de modifications non nécessaires du schéma YAML.
* Ne supprime pas de tests existants pour faire passer la suite.
* Préserve les conventions, interfaces et responsabilités déjà présentes.
* Si une décision d’architecture est nécessaire, choisir la modification minimale et la justifier.

## 10. Validation finale obligatoire

Depuis la racine du dépôt :

```bash
gofmt -w <fichiers-Go-modifiés>
go test ./... -count=1
go vet ./...
git diff --check
git status --short
git diff --stat
git diff
```

Exécuter les commandes réellement. Si une commande échoue, rapporter sa sortie et sa cause probable. Ne pas déclarer la tâche terminée tant que les échecs introduits par les modifications ne sont pas résolus.

## 11. Rapport à retourner

Fournir :

1. L’état initial et les composants réutilisés.
2. Les fichiers modifiés.
3. Le parcours réel désormais relié.
4. Les fonctionnalités encore absentes.
5. Les commandes exécutées et leurs résultats réels.
6. La liste des tests ajoutés.
7. Les limites et risques restants.
8. Le SHA du commit uniquement si un commit a réellement été créé.

## Critère d’acceptation

Un parcours démontrable doit partir d’une infrastructure valide, construire un plan, produire un artefact réel avec une provenance traçable, et restituer honnêtement le résultat de chaque étape.

Si l’exécution sur un équipement ou la vérification indépendante n’est pas disponible, le système doit le montrer explicitement. Aucune réussite ne doit être inventée.
