# InfraFlow — Finalisation ciblée avant soutenance

Tu travailles sur le dépôt Nouments/infraflow, branche develop, dernier commit distant examiné d3d6fbb71bb59d0dbaf8031b78fc70de0b09e716.

CONTEXTE CONFIRMÉ DANS LE CODE :
- Générateur déclaratif : internal/adapters/generation/declarative.go
- Catalogue embarqué : internal/adapters/catalog/catalog.go
- Fiche actuelle : internal/adapters/catalog/data/cisco-isr-1100.yaml
- Template actuel : internal/adapters/generation/templates/vendors/cisco/iosxe/isr-1100/
- Exemple valide : examples/network-deployment.yaml
- Exemple incompatible : examples/network-deployment-unsupported-ospf.yaml
- Tests : internal/adapters/generation/declarative_test.go et internal/adapters/catalog/catalog_test.go
- Commande : go run ./provider/cmd generate-device-config

OBJECTIF :
Finaliser la démonstration existante, sans réécrire le moteur ni modifier inutilement l'architecture.

ÉTAPE 1 — VÉRIFICATION
1. Vérifie l'état réel de la branche et les modifications locales.
2. Exécute les tests ciblés du catalogue et de la génération déclarative.
3. Exécute les commandes de validation et de génération documentées dans le README.
4. Vérifie le contenu réel des fichiers produits et le rapport JSON.
5. Vérifie que le scénario OSPF échoue explicitement et ne publie aucune configuration partielle.
6. N'annonce aucun test comme réussi s'il n'a pas été exécuté.

ÉTAPE 2 — CORRECTIONS MINIMALES
Corrige uniquement les défauts reproductibles qui bloquent le scénario de démonstration. Préserve les interfaces existantes, les tests, les mécanismes de sécurité et le comportement fail-closed. N'introduis pas de régression dans les autres packages.

ÉTAPE 3 — CATALOGUE MATÉRIEL
Complète uniquement les données réellement vérifiables :
- conserve les références officielles ;
- récupère une fiche technique et une image locale uniquement si les ressources sont disponibles et leur redistribution est autorisée ;
- vérifie que les fichiers locaux correspondent aux références de la fiche YAML ;
- ne fabrique aucune capacité, aucun modèle, aucune image et aucune fiche technique ;
- conserve un statut explicite lorsqu'une ressource n'a pas été récupérée.

N'ajoute pas sept constructeurs ou modèles fictivement compatibles. N'étends pas le template Cisco à OSPF, VLAN ou routes statiques sans implémentation et tests spécifiques.

ÉTAPE 4 — DÉMONSTRATION
Assure-toi que le README présente un scénario reproductible :
YAML → validation → catalogue matériel → résolution du template → configuration générée → rapport.

Le rapport doit distinguer clairement :
- la validation du document ;
- la compatibilité de déclaration avec le template ;
- la validation syntaxique des commandes, si elle existe ;
- la validation sur un équipement physique, si elle a réellement eu lieu.

ÉTAPE 5 — RAPPORT FINAL
Fournis :
- les fichiers modifiés ;
- les commandes exécutées ;
- les résultats réels des tests ;
- les fichiers générés ;
- les défauts corrigés ;
- les limites restantes avant soutenance.

Ne refonds pas le projet. Ne crée pas de nouvelle interface web. Ne lance pas de migration SQL. Ne touche pas aux composants sans rapport avec cette démonstration. N'écrase pas les modifications locales de l'utilisateur.