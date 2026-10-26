À partir de l'audit du dépôt InfraFlow, implémente le chemin fonctionnel YAML déclaratif → validation → résolution du template → génération de configuration.

OBJECTIF :
L'utilisateur décrit l'état réseau souhaité dans un fichier YAML. InfraFlow transforme cette description en configuration réseau spécifique à un équipement, sans que l'utilisateur ait à écrire lui-même toutes les commandes constructeur.

1. FORMAT DÉCLARATIF

Réutilise le schéma YAML existant s'il est exploitable. Sinon, crée un schéma minimal et documenté comprenant :
- version du document ;
- site ;
- équipements ;
- constructeur, modèle et système d'exploitation ;
- interfaces et adresses IP ;
- VLAN et paramètres associés ;
- routage statique ou OSPF lorsque le template correspondant existe ;
- options et variables nécessaires à la génération.

N'implémente pas des fonctionnalités que les templates existants ne savent pas traiter.

2. VALIDATION

- Rejeter les YAML mal formés.
- Valider les champs obligatoires et les types.
- Vérifier les références aux équipements et aux templates.
- Détecter les paramètres réseau incohérents lorsque c'est possible.
- Retourner des erreurs explicites avec le chemin du champ concerné.
- Ne jamais produire silencieusement une configuration partielle en cas d'erreur.

3. MOTEUR DE GÉNÉRATION

- Charger les templates prédéfinis depuis les fichiers du dépôt.
- Utiliser le moteur de templates déjà présent ; sinon, privilégier text/template de la bibliothèque standard Go.
- Résoudre le template à partir du constructeur, du modèle, de l'OS et des fonctionnalités requises.
- Injecter les valeurs validées du YAML.
- Générer des fichiers réels dans un répertoire de sortie déterministe.
- Séparer les données déclaratives, la logique de compatibilité et les templates.
- Ne jamais construire les commandes en concaténant aveuglément des entrées utilisateur.
- Échapper ou valider les valeurs selon le contexte du template.

4. RÉSULTAT

La génération doit fournir :
- la configuration propre à l'équipement ;
- l'identité du template utilisé ;
- les variables résolues ;
- les éventuelles incompatibilités ;
- un rapport de génération.

5. TESTS OBLIGATOIRES

- YAML valide donnant une configuration non vide.
- YAML invalide rejeté.
- Template inexistant signalé clairement.
- Fonctionnalité non supportée rejetée.
- Même entrée et même version de template donnant le même résultat.
- Vérification du contenu des configurations générées.

6. INTÉGRATION

Intègre cette fonctionnalité dans les points d'entrée et la TUI existants lorsqu'ils sont déjà adaptés. Ajoute une commande CLI uniquement si elle est nécessaire et cohérente avec l'architecture.

Ne refonds pas l'application. Lance les tests Go pertinents, corrige les régressions et fournis les commandes exactes permettant de reproduire la génération.

À la fin, indique les fichiers modifiés, les tests exécutés, leurs résultats et les limites restantes. Ne prétends pas avoir testé sur un véritable équipement si cela n'a pas été fait.

apiVersion: infraflow/v1
kind: NetworkDeployment

metadata:
  name: lab-demo

spec:
  site: lab

  devices:
    - name: R1
      vendor: cisco
      model: isr-1100
      os: ios-xe

      interfaces:
        - name: GigabitEthernet0/0/0
          ipv4: 192.168.10.1/24
          enabled: true

      routing:
        ospf:
          process_id: 10
          router_id: 10.255.0.1
          networks:
            - prefix: 192.168.10.0/24
              area: 0




Implémente ou finalise le catalogue local des équipements réseau d'InfraFlow en réutilisant la structure du dépôt et les outils existants.

OBJECTIF :
Disposer d'une base de données structurée de matériels réseau permettant de décrire les équipements, leurs capacités, leurs systèmes d'exploitation et leurs fiches techniques, puis d'utiliser ces informations pendant la validation et la génération de configurations.

PÉRIMÈTRE INITIAL :
Priorité aux modèles déjà présents dans le catalogue et aux équipements utilisés dans le laboratoire :
- Cisco ISR 1100 ;
- Cisco ISR 4000 ;
- Cisco Catalyst 2960 ;
- Cisco Catalyst 9200 ;
- Cisco Catalyst 9300 ;
- équipements MikroTik RouterOS 7.x ;
- FortiGate/FortiOS 7.x uniquement lorsque les données et templates correspondants sont disponibles.

N'ajoute pas artificiellement tous ces modèles si leurs informations ne peuvent pas être vérifiées.

DONNÉES À GÉRER :
Chaque fiche matériel doit inclure, selon les informations disponibles :
- identifiant stable ;
- constructeur ;
- nom commercial et modèle ;
- catégorie : routeur, commutateur, pare-feu ou autre ;
- systèmes d'exploitation et versions compatibles ;
- interfaces et capacités connues ;
- fonctionnalités réseau prises en charge ;
- identifiants des templates compatibles ;
- URL officielle de la documentation ;
- références aux fiches techniques locales ;
- références aux images locales ;
- source, date de vérification et état de validation des données.

ARCHITECTURE :
- Réutiliser le répertoire catalogue existant, s'il est présent.
- Privilégier YAML ou JSON pour les fiches et un schéma de validation explicite.
- Fournir une interface Go permettant de rechercher un équipement par identifiant, constructeur ou modèle.
- Vérifier les références aux templates et aux documents.
- Distinguer les données vérifiées des données inconnues. Ne pas inventer les ports, les capacités ou les compatibilités.
- Utiliser les données du catalogue dans le moteur de génération pour vérifier la compatibilité entre équipement, OS, fonctionnalités demandées et template.

FICHES TECHNIQUES ET IMAGES :
- Utiliser des documents et images réels provenant de sources officielles lorsque disponibles et autorisés.
- Enregistrer les ressources localement dans l'arborescence du catalogue.
- Ne pas présenter une URL comme un fichier téléchargé.
- Ne pas créer de fausses fiches techniques ou d'images fictives.
- En cas d'échec de récupération, conserver la référence officielle et marquer clairement la ressource comme indisponible localement.
- Documenter la provenance et les conditions de redistribution des fichiers.

INTÉGRATION :
Le moteur doit pouvoir charger le catalogue, retrouver un équipement, déterminer ses capacités connues et refuser une génération incompatible.

TESTS :
- Chargement du catalogue.
- Validation des fiches.
- Recherche par identifiant et modèle.
- Référence à un template absent.
- Fiche technique locale absente.
- Fonctionnalité déclarée mais non prise en charge.

LIVRABLE :
Un catalogue exploitable par l'application, quelques fiches réellement renseignées, une intégration effective au générateur et une documentation courte.

Ne migre pas vers une base de données externe pour la seule démonstration de demain. Ne remplace pas les fichiers existants sans justification. Exécute les tests et rapporte les résultats réels.




Implémente la résolution et la comparaison fonctionnelle des templates prédéfinis dans InfraFlow.

OBJECTIF :
À partir d'une description réseau YAML et d'un équipement du catalogue, identifier les templates compatibles et sélectionner le meilleur candidat sans inventer de compatibilité.

FONCTIONNEMENT :
1. Charger les templates existants.
2. Lire leurs métadonnées : identifiant, constructeur, modèles ou familles supportés, OS et versions, fonctionnalités requises et variables attendues.
3. Comparer ces informations aux équipements du catalogue et aux besoins du YAML.
4. Éliminer les candidats incompatibles.
5. Calculer un score explicable pour les candidats restants.
6. Générer la configuration uniquement avec un template compatible.

SCORE :
Définis un score déterministe documenté sur 100, basé uniquement sur les critères réellement disponibles :
- compatibilité constructeur et modèle ;
- compatibilité OS/version ;
- couverture des fonctionnalités demandées ;
- disponibilité des variables nécessaires ;
- validité et état du template.

La compatibilité obligatoire ne doit pas pouvoir être compensée par un bon score sur les autres critères. Un template incompatible doit être rejeté, quel que soit son score.

RÉSULTAT :
Présenter :
- les candidats évalués ;
- le score de chaque candidat admissible ;
- les critères et justifications du score ;
- les incompatibilités des candidats rejetés ;
- le template retenu et la raison de son choix.

Ne confonds pas une absence de métadonnées avec une compatibilité confirmée. Si aucune sélection sûre n'est possible, explique pourquoi et bloque la génération.

TESTS :
- Sélection du template compatible attendu.
- Rejet d'un constructeur incompatible.
- Rejet d'une version d'OS non supportée.
- Rejet d'une fonctionnalité absente.
- Score déterministe.
- Absence de template compatible.
- Résultat explicable et reproductible.

Réutilise les structures, le moteur de templates et les interfaces Go déjà présents. N'ajoute pas de système de plugins ou d'architecture complexe pour cette tâche. Fournis un exemple complet de YAML, le template sélectionné, le score et la configuration générée.


Prépare une démonstration reproductible de bout en bout pour InfraFlow, à partir des fonctionnalités réellement implémentées dans le dépôt.

SCÉNARIO :
Un utilisateur définit un équipement réseau et ses besoins dans un fichier YAML. InfraFlow charge le catalogue, valide les données, vérifie les capacités de l'équipement, sélectionne un template compatible, génère la configuration et affiche un rapport.

À FAIRE :
1. Créer ou corriger un exemple YAML réaliste.
2. Vérifier que le modèle déclaré existe dans le catalogue.
3. Vérifier que le template sélectionné correspond réellement au constructeur et à l'OS.
4. Générer les fichiers de configuration dans un répertoire de sortie clair.
5. Ajouter un rapport indiquant les validations, le template retenu, les fonctionnalités traitées et les éventuelles limites.
6. Ajouter un scénario négatif où une demande incompatible est correctement rejetée.
7. Intégrer le scénario à la CLI ou à la TUI existante sans refonte.
8. Mettre à jour le README avec les commandes exactes de lancement et de démonstration.

VALIDATION :
- Exécuter go test ./... depuis le bon module ou workspace.
- Exécuter go vet ./... si applicable.
- Corriger les régressions introduites par les changements.
- Vérifier les fichiers effectivement générés.
- Ne pas simuler des tests sur du matériel physique.
- Ne pas utiliser de métriques fictives ni annoncer une réussite sans preuve.

LIVRABLES :
- Commandes exactes pour reproduire la démonstration.
- Fichiers d'entrée et de sortie.
- Résultats des tests réellement exécutés.
- Liste des fonctionnalités terminées.
- Liste explicite des fonctionnalités partielles ou non implémentées.

PRIORITÉ :
La démonstration YAML → catalogue → template → configuration générée doit fonctionner avant toute amélioration visuelle, nouvelle fonctionnalité ou refonte documentaire.