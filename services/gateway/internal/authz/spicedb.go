package authz

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	pb "github.com/authzed/authzed-go/proto/authzed/api/v1"
	authzed "github.com/authzed/authzed-go/v1"
	"github.com/authzed/grpcutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// AuthzClient wraps the SpiceDB gRPC client for permission checks.
type AuthzClient struct {
	client *authzed.Client
}

// NewAuthzClient creates a new SpiceDB client connected via gRPC.
// Uses insecure transport (TLS disabled) with bearer token auth.
func NewAuthzClient(endpoint, presharedKey string) (*AuthzClient, error) {
	client, err := authzed.NewClient(
		endpoint,
		grpcutil.WithInsecureBearerToken(presharedKey),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("spicedb client: %w", err)
	}

	return &AuthzClient{client: client}, nil
}

// CheckDocumentAccess checks if a user has "view" permission on a document.
// Returns false if SpiceDB denies access or is unreachable (fail-closed).
func (a *AuthzClient) CheckDocumentAccess(ctx context.Context, userID, documentID string) (bool, error) {
	resp, err := a.client.CheckPermission(ctx, &pb.CheckPermissionRequest{
		Resource: &pb.ObjectReference{
			ObjectType: "document",
			ObjectId:   documentID,
		},
		Permission: "view",
		Subject: &pb.SubjectReference{
			Object: &pb.ObjectReference{
				ObjectType: "user",
				ObjectId:   userID,
			},
		},
	})
	if err != nil {
		return false, fmt.Errorf("spicedb check permission: %w", err)
	}

	return resp.Permissionship == pb.CheckPermissionResponse_PERMISSIONSHIP_HAS_PERMISSION, nil
}

// GetUserTeams returns the list of teams a user is a member of.
// Uses ReadRelationships to find all team#member relationships for the user.
func (a *AuthzClient) GetUserTeams(ctx context.Context, userID string) ([]string, error) {
	stream, err := a.client.ReadRelationships(ctx, &pb.ReadRelationshipsRequest{
		RelationshipFilter: &pb.RelationshipFilter{
			ResourceType:     "team",
			OptionalRelation: "member",
			OptionalSubjectFilter: &pb.SubjectFilter{
				SubjectType:       "user",
				OptionalSubjectId: userID,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("spicedb read relationships: %w", err)
	}

	var teams []string
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("spicedb stream recv: %w", err)
		}
		teams = append(teams, resp.Relationship.Resource.ObjectId)
	}

	return teams, nil
}

// CreateDocumentRelationships writes owner and viewer relationships for a document.
// Called during document upload to register permissions in SpiceDB.
func (a *AuthzClient) CreateDocumentRelationships(ctx context.Context, documentID, ownerUserID string, viewerTeams []string) error {
	updates := make([]*pb.RelationshipUpdate, 0, 1+len(viewerTeams))

	// Owner relationship
	updates = append(updates, &pb.RelationshipUpdate{
		Operation: pb.RelationshipUpdate_OPERATION_TOUCH,
		Relationship: &pb.Relationship{
			Resource: &pb.ObjectReference{
				ObjectType: "document",
				ObjectId:   documentID,
			},
			Relation: "owner",
			Subject: &pb.SubjectReference{
				Object: &pb.ObjectReference{
					ObjectType: "user",
					ObjectId:   ownerUserID,
				},
			},
		},
	})

	// Viewer relationships (team-based)
	for _, team := range viewerTeams {
		updates = append(updates, &pb.RelationshipUpdate{
			Operation: pb.RelationshipUpdate_OPERATION_TOUCH,
			Relationship: &pb.Relationship{
				Resource: &pb.ObjectReference{
					ObjectType: "document",
					ObjectId:   documentID,
				},
				Relation: "viewer",
				Subject: &pb.SubjectReference{
					Object: &pb.ObjectReference{
						ObjectType: "team",
						ObjectId:   team,
					},
					OptionalRelation: "member",
				},
			},
		})
	}

	_, err := a.client.WriteRelationships(ctx, &pb.WriteRelationshipsRequest{
		Updates: updates,
	})
	if err != nil {
		return fmt.Errorf("spicedb write relationships: %w", err)
	}

	slog.Info("spicedb relationships created",
		"document", documentID,
		"owner", ownerUserID,
		"viewer_teams", viewerTeams,
	)

	return nil
}
