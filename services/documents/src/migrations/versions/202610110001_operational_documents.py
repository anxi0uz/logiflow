"""Allow immutable documents per business event, including multiple handovers."""

import sqlalchemy as sa
from alembic import op

revision = "202610110001"
down_revision = "202609240001"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.add_column("documents", sa.Column("reference_id", sa.Uuid(), nullable=True))
    op.execute("UPDATE documents SET reference_id = order_id")
    op.alter_column("documents", "reference_id", nullable=False)
    op.drop_constraint("uq_documents_order_type", "documents", type_="unique")
    op.create_unique_constraint(
        "uq_documents_reference_type", "documents", ["reference_id", "type"]
    )

    op.create_table(
        "document_recipients",
        sa.Column(
            "document_id",
            sa.Uuid(),
            sa.ForeignKey("documents.id", ondelete="CASCADE"),
            primary_key=True,
        ),
        sa.Column("user_id", sa.Uuid(), primary_key=True),
    )
    op.create_index(
        "ix_document_recipients_user_id", "document_recipients", ["user_id"]
    )


def downgrade() -> None:
    # Deliberately fail on multiple same-type documents per order instead of
    # silently deleting issued historical records to make the downgrade fit.
    op.create_unique_constraint(
        "uq_documents_order_type", "documents", ["order_id", "type"]
    )
    op.drop_constraint("uq_documents_reference_type", "documents", type_="unique")
    op.drop_column("documents", "reference_id")
    op.drop_table("document_recipients")
